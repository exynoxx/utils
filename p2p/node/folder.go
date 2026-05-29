package node

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"p2p/protocol"
	"p2p/share"
	"p2p/transfer"
)

// sharedFolder tracks a named folder that is synchronised with peers.
type sharedFolder struct {
	name    string
	dir     string
	watcher *share.Watcher

	mu    sync.RWMutex
	peers map[string]bool // peer addr → participates in this folder
}

func newSharedFolder(name, dir string) *sharedFolder {
	return &sharedFolder{
		name:  name,
		dir:   dir,
		peers: make(map[string]bool),
	}
}

func (sf *sharedFolder) addPeer(addr string) {
	sf.mu.Lock()
	sf.peers[addr] = true
	sf.mu.Unlock()
}

func (sf *sharedFolder) removePeer(addr string) {
	sf.mu.Lock()
	delete(sf.peers, addr)
	sf.mu.Unlock()
}

func (sf *sharedFolder) peerAddrs() []string {
	sf.mu.RLock()
	defer sf.mu.RUnlock()
	addrs := make([]string, 0, len(sf.peers))
	for addr := range sf.peers {
		addrs = append(addrs, addr)
	}
	return addrs
}

// --- Node methods ---

// initSharedFolders creates watchers for all configured shared folders and
// starts the change-broadcast loops.
func (n *Node) initSharedFolders() {
	n.folders = make(map[string]*sharedFolder)
	for _, name := range n.cfg.SharedFolders {
		dir := filepath.Join(".", name)
		sf := newSharedFolder(name, dir)
		w := share.New(name, dir)
		sf.watcher = w
		if err := w.Start(); err != nil {
			fmt.Printf("[warn] shared folder %q unavailable: %v\n", name, err)
			continue
		}
		n.folders[name] = sf
		go n.folderChangeLoop(sf)
		fmt.Printf("[folder] sharing %q → %s\n", name, dir)
	}
}

// folderChangeLoop reads watcher events for sf and broadcasts them to peers
// that share the same folder.
func (n *Node) folderChangeLoop(sf *sharedFolder) {
	for {
		select {
		case <-n.quit:
			return
		case ch, ok := <-sf.watcher.Changes:
			if !ok {
				return
			}
			switch ch.Kind {
			case share.Added, share.Modified:
				info, err := os.Stat(ch.AbsPath)
				if err != nil {
					continue
				}
				modTime := info.ModTime().Unix()
				for _, addr := range sf.peerAddrs() {
					p := n.peers.Get(addr)
					if p == nil {
						continue
					}
					go func(pCopy *Peer, path, rel string, mt int64) {
						if err := transfer.SendFolderFile(sf.name, rel, path, mt, pCopy.Addr, pCopy.Send, nil); err != nil {
							fmt.Printf("[warn] folder send %s: %v\n", rel, err)
						}
					}(p, ch.AbsPath, ch.RelPath, modTime)
				}
			case share.Deleted:
				msg, err := protocol.NewMessage(protocol.MsgFolderDelete, protocol.FolderDeletePayload{
					Folder:  sf.name,
					RelPath: ch.RelPath,
				})
				if err != nil {
					continue
				}
				for _, addr := range sf.peerAddrs() {
					if p := n.peers.Get(addr); p != nil {
						p.Send(msg)
					}
				}
			}
			// Notify registered callbacks (UI updates).
			for _, fn := range n.onFolderChange {
				fn(sf.name, ch.RelPath, ch.AbsPath, ch.Kind == share.Deleted)
			}
		}
	}
}

// sendFolderAnnounce tells a peer which folders this node shares.
func (n *Node) sendFolderAnnounce(p *Peer) {
	names := make([]string, 0, len(n.folders))
	for name := range n.folders {
		names = append(names, name)
	}
	if len(names) == 0 {
		return
	}
	msg, err := protocol.NewMessage(protocol.MsgFolderAnnounce, protocol.FolderAnnouncePayload{Names: names})
	if err != nil {
		return
	}
	p.Send(msg)
}

// sendFolderAll pushes every file currently in sf to peer p (initial sync).
func (n *Node) sendFolderAll(sf *sharedFolder, p *Peer) {
	filepath.WalkDir(sf.dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(sf.dir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return nil
		}
		go func(absPath, relPath string, mt int64) {
			if err := transfer.SendFolderFile(sf.name, relPath, absPath, mt, p.Addr, p.Send, nil); err != nil {
				fmt.Printf("[warn] folder initial sync %s: %v\n", relPath, err)
			}
		}(path, rel, info.ModTime().Unix())
		return nil
	})
}

// handleFolderAnnounce processes an incoming folder_announce message.
func (n *Node) handleFolderAnnounce(p *Peer, msg protocol.Message) {
	var pl protocol.FolderAnnouncePayload
	if err := json.Unmarshal(msg.Payload, &pl); err != nil {
		return
	}
	for _, name := range pl.Names {
		sf, ok := n.folders[name]
		if !ok {
			continue
		}
		sf.addPeer(p.Addr)
		// Initial sync: push all our current files to the new peer.
		go n.sendFolderAll(sf, p)
	}
}

// handleFolderFileMeta processes an incoming folder_file_meta message.
// If meta.Folder matches a shared folder on this node, the file is written
// into that folder (live-sync path). Otherwise this is an ad-hoc folder
// transfer and the file is dropped into downloads/<folder-name>/<relPath>.
func (n *Node) handleFolderFileMeta(p *Peer, msg protocol.Message) {
	var meta protocol.FolderFileMetaPayload
	if err := json.Unmarshal(msg.Payload, &meta); err != nil {
		return
	}
	if sf, ok := n.folders[meta.Folder]; ok {
		n.recv.HandleFolderMeta(msg.Payload, sf.dir, p.Addr, func(folderName, relPath, absPath string) {
			// Suppress watcher re-broadcast for this freshly-written file.
			sf.watcher.Refresh(relPath)
			for _, fn := range n.onFolderChange {
				fn(folderName, relPath, absPath, false)
			}
		})
		return
	}
	// Ad-hoc folder send: place files under downloads/<folder-name>/.
	safeName := filepath.Base(filepath.Clean(meta.Folder))
	if safeName == "" || safeName == "." || safeName == ".." {
		fmt.Printf("[warn] unsafe folder name rejected: %q\n", meta.Folder)
		return
	}
	dir := filepath.Join(n.cfg.DownloadsDir, safeName)
	n.recv.HandleFolderMeta(msg.Payload, dir, p.Addr, func(_, _, absPath string) {
		for _, fn := range n.onFile {
			fn(absPath)
		}
	})
}

// handleFolderDelete processes an incoming folder_delete message.
func (n *Node) handleFolderDelete(msg protocol.Message) {
	var pl protocol.FolderDeletePayload
	if err := json.Unmarshal(msg.Payload, &pl); err != nil {
		return
	}
	sf, ok := n.folders[pl.Folder]
	if !ok {
		return
	}
	relPath := filepath.FromSlash(filepath.Clean(pl.RelPath))
	if filepath.IsAbs(relPath) || relPath == ".." || (len(relPath) >= 3 && relPath[:3] == ".."+string(filepath.Separator)) {
		fmt.Printf("[warn] unsafe folder delete path rejected: %s\n", pl.RelPath)
		return
	}
	absPath := filepath.Join(sf.dir, relPath)
	if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
		fmt.Printf("[warn] folder delete %s: %v\n", absPath, err)
	}
	normRel := filepath.ToSlash(relPath)
	sf.watcher.RefreshDelete(normRel)
	for _, fn := range n.onFolderChange {
		fn(pl.Folder, normRel, "", true)
	}
}
