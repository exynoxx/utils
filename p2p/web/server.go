// Package web serves a live browser UI that mirrors the P2P node state.
// It exposes an SSE stream for real-time events and a small REST API for
// sending chats, files, and listing received files.
package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	_ "embed"

	"p2p/node"
)

//go:embed ui.html
var uiHTML []byte

//go:embed phone.html
var phoneHTML []byte

// sseHub fans out SSE messages to all connected browser clients.
type sseHub struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

func newSSEHub() *sseHub {
	return &sseHub{clients: make(map[chan string]struct{})}
}

func (h *sseHub) subscribe() chan string {
	ch := make(chan string, 64)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *sseHub) unsubscribe(ch chan string) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
}

func (h *sseHub) broadcast(event string, data []byte) {
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default: // slow client â€” drop rather than block
		}
	}
}

// Server bridges the Node to a live browser UI.
type Server struct {
	node         *node.Node
	hub          *sseHub
	downloadsDir string
}

// New creates a Server wired to n. downloadsDir is the folder that received
// files are written to and listed from. Registers node callbacks immediately;
// call ListenAndServe to start the HTTP server.
func New(n *node.Node, downloadsDir string) *Server {
	s := &Server{node: n, hub: newSSEHub(), downloadsDir: downloadsDir}

	n.OnChat(func(nick, text string, ts time.Time) {
		b, _ := json.Marshal(map[string]any{
			"nick": nick,
			"text": text,
			"time": ts.Format("15:04"),
		})
		s.hub.broadcast("chat", b)
	})
	n.OnFile(func(path string) {
		b, _ := json.Marshal(map[string]string{"path": path})
		s.hub.broadcast("file_done", b)
		// Push updated file list to all clients.
		if fl, err := s.fileListJSON(); err == nil {
			s.hub.broadcast("files_changed", fl)
		}
	})
	n.OnPeer(func(info node.PeerInfo) {
		b, _ := json.Marshal(info)
		s.hub.broadcast("peer_join", b)
	})
	n.OnPeerLeave(func(addr string) {
		b, _ := json.Marshal(map[string]string{"addr": addr})
		s.hub.broadcast("peer_leave", b)
	})

	n.OnFolderChange(func(folderName, relPath, absPath string, deleted bool) {
		if deleted {
			b, _ := json.Marshal(map[string]string{"folder": folderName, "rel_path": relPath})
			s.hub.broadcast("folder_delete", b)
		} else {
			b, _ := json.Marshal(map[string]string{"folder": folderName, "rel_path": relPath})
			s.hub.broadcast("folder_change", b)
		}
		// Push the updated file list for this folder.
		if fl, err := s.folderFilesJSON(folderName); err == nil {
			b2, _ := json.Marshal(map[string]json.RawMessage{"folder": mustJSON(folderName), "files": fl})
			s.hub.broadcast("folder_files", b2)
		}
	})

	n.OnProgress(func(name string, sent, total int64, recv bool, peerAddr string) {
		var pct float64
		if total > 0 {
			pct = float64(sent) / float64(total) * 100
		}
		b, _ := json.Marshal(map[string]any{
			"name":  name,
			"pct":   pct,
			"recv":  recv,
			"peer":  peerAddr,
			"bytes": sent,
			"total": total,
		})
		s.hub.broadcast("transfer_progress", b)
	})

	return s
}

// ListenAndServe starts the HTTP server on addr (e.g. ":8080"). Blocks.
func (s *Server) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleUI)
	mux.HandleFunc("GET /phone", s.handlePhoneUI)
	mux.HandleFunc("GET /events", s.handleSSE)
	mux.HandleFunc("GET /state", s.handleState)
	mux.HandleFunc("POST /chat", s.handleChat)
	mux.HandleFunc("POST /file", s.handleFile)
	mux.HandleFunc("POST /folder", s.handleFolder)
	mux.HandleFunc("POST /upload", s.handleUpload)
	mux.HandleFunc("GET /files", s.handleFilesList)
	mux.HandleFunc("GET /files/open", s.handleFileOpen)
	mux.HandleFunc("GET /files/opendir", s.handleFileOpenDir)
	mux.HandleFunc("GET /files/download", s.handleFileDownload)
	mux.HandleFunc("GET /folders", s.handleFoldersList)
	return http.ListenAndServe(addr, mux)
}

// --- snapshot types ---

type selfInfo struct {
	Nick    string `json:"nick"`
	Addr    string `json:"addr"`
	Crypto  bool   `json:"crypto"`
	ExtAddr string `json:"ext_addr,omitempty"`
}

type initState struct {
	Self    selfInfo          `json:"self"`
	Peers   []node.PeerInfo   `json:"peers"`
	Folders []folderStateInfo `json:"folders,omitempty"`
}

// folderStateInfo carries a folder name + its current file list for the init event.
type folderStateInfo struct {
	Name  string      `json:"name"`
	Files []fileEntry `json:"files"`
}

func (s *Server) snapshot() initState {
	peers := s.node.Peers()
	if peers == nil {
		peers = []node.PeerInfo{}
	}
	folderInfos := s.node.SharedFolderInfos()
	folderStates := make([]folderStateInfo, 0, len(folderInfos))
	for _, fi := range folderInfos {
		files, _ := s.readDirFiles(fi.Dir)
		folderStates = append(folderStates, folderStateInfo{Name: fi.Name, Files: files})
	}
	return initState{
		Self: selfInfo{
			Nick:    s.node.Nick(),
			Addr:    s.node.ListenAddr(),
			Crypto:  s.node.CryptoEnabled(),
			ExtAddr: s.node.ExternalAddr(),
		},
		Peers:   peers,
		Folders: folderStates,
	}
}

// --- file list helper ---

type fileEntry struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Mod  string `json:"mod"`
}

// readDirFiles returns a sorted (newest-first) flat list of files in dir.
func (s *Server) readDirFiles(dir string) ([]fileEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []fileEntry{}, nil
		}
		return nil, err
	}
	files := make([]fileEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileEntry{
			Name: e.Name(),
			Size: info.Size(),
			Mod:  info.ModTime().Format(time.RFC3339),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Mod > files[j].Mod })
	return files, nil
}

func (s *Server) fileListJSON() ([]byte, error) {
	files, err := s.readDirFiles(s.downloadsDir)
	if err != nil {
		return nil, err
	}
	return json.Marshal(files)
}

// folderFilesJSON returns the JSON-encoded file list for the named shared folder.
func (s *Server) folderFilesJSON(folderName string) ([]byte, error) {
	for _, fi := range s.node.SharedFolderInfos() {
		if fi.Name == folderName {
			files, err := s.readDirFiles(fi.Dir)
			if err != nil {
				return nil, err
			}
			return json.Marshal(files)
		}
	}
	return json.Marshal([]fileEntry{})
}

// mustJSON marshals v to JSON, panicking on error (only used for string literals).
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// --- handlers ---

func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(uiHTML)
}

// handlePhoneUI serves the mobile-friendly page used to send files to / grab
// files from this PC over the LAN.
func (s *Server) handlePhoneUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(phoneHTML)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Bootstrap new client: push full state + current file list.
	snap := s.snapshot()
	b, _ := json.Marshal(snap)
	fmt.Fprintf(w, "event: init\ndata: %s\n\n", b)
	if fl, err := s.fileListJSON(); err == nil {
		fmt.Fprintf(w, "event: files_changed\ndata: %s\n\n", fl)
	}
	flusher.Flush()

	ch := s.hub.subscribe()
	defer s.hub.unsubscribe(ch)

	for {
		select {
		case msg := <-ch:
			fmt.Fprint(w, msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.snapshot())
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		http.Error(w, "text required", http.StatusBadRequest)
		return
	}
	s.node.SendChat(req.Text)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	peer := r.URL.Query().Get("peer")
	if peer == "" {
		http.Error(w, "peer query param required", http.StatusBadRequest)
		return
	}

	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "expected multipart body: "+err.Error(), http.StatusBadRequest)
		return
	}

	dir, err := os.MkdirTemp("", "p2p-upload-*")
	if err != nil {
		http.Error(w, "temp dir: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var tmpPath, name string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			os.RemoveAll(dir)
			http.Error(w, "read part: "+err.Error(), http.StatusBadRequest)
			return
		}
		if part.FormName() != "file" {
			part.Close()
			continue
		}
		name = filepath.Base(part.FileName())
		if name == "." || name == ".." || name == "" {
			part.Close()
			os.RemoveAll(dir)
			http.Error(w, "invalid filename", http.StatusBadRequest)
			return
		}
		tmpPath = filepath.Join(dir, name)
		dst, oerr := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY, 0o600)
		if oerr != nil {
			part.Close()
			os.RemoveAll(dir)
			http.Error(w, "temp file: "+oerr.Error(), http.StatusInternalServerError)
			return
		}
		// Stream directly from request body to disk — no in-memory spool,
		// no double-write through Go's multipart temp file.
		_, cerr := io.Copy(dst, part)
		dst.Close()
		part.Close()
		if cerr != nil {
			os.RemoveAll(dir)
			http.Error(w, "write temp: "+cerr.Error(), http.StatusInternalServerError)
			return
		}
		break
	}
	if tmpPath == "" {
		os.RemoveAll(dir)
		http.Error(w, "file field required", http.StatusBadRequest)
		return
	}

	// Accepted: the actual send runs in the background so we don't block
	// the browser waiting for a potentially long transfer.
	w.WriteHeader(http.StatusAccepted)

	go func() {
		defer os.RemoveAll(dir)
		if err := s.node.SendFile(peer, tmpPath); err != nil {
			fmt.Printf("[warn] web file send to %s: %v\n", peer, err)
		}
	}()
}

func (s *Server) handleFolder(w http.ResponseWriter, r *http.Request) {
	peer := r.URL.Query().Get("peer")
	if peer == "" {
		http.Error(w, "peer query param required", http.StatusBadRequest)
		return
	}
	folderName := filepath.Base(filepath.Clean(r.URL.Query().Get("name")))
	if folderName == "" || folderName == "." || folderName == ".." {
		http.Error(w, "name query param required", http.StatusBadRequest)
		return
	}

	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "expected multipart body: "+err.Error(), http.StatusBadRequest)
		return
	}

	stageDir, err := os.MkdirTemp("", "p2p-folder-*")
	if err != nil {
		http.Error(w, "temp dir: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Pipeline: as soon as a file finishes staging, hand it to the sender so
	// peer transfer begins while the rest of the upload is still streaming in.
	// Small buffer = flow control: if the network is slower than disk, the
	// multipart read back-pressures and %TEMP% won't fill with the entire
	// upload before any bytes leave the machine.
	pending := make(chan node.FolderFileEntry, 2)
	senderDone := make(chan struct{})
	go func() {
		defer close(senderDone)
		defer os.RemoveAll(stageDir)
		for e := range pending {
			if err := s.node.SendFolder(peer, folderName, []node.FolderFileEntry{e}); err != nil {
				fmt.Printf("[warn] web folder send to %s: %v\n", peer, err)
			}
			os.Remove(e.AbsPath)
		}
	}()

	abort := func(status int, msg string) {
		close(pending)
		<-senderDone
		http.Error(w, msg, status)
	}

	sawAny := false
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			abort(http.StatusBadRequest, "read part: "+err.Error())
			return
		}
		// FormName carries the file's relative path (e.g. "subdir/img.png").
		rel := strings.TrimPrefix(part.FormName(), folderName+"/")
		rel = filepath.ToSlash(filepath.Clean(rel))
		if rel == "" || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			part.Close()
			abort(http.StatusBadRequest, "invalid rel path: "+part.FormName())
			return
		}
		absPath := filepath.Join(stageDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(absPath), 0o750); err != nil {
			part.Close()
			abort(http.StatusInternalServerError, "stage dir: "+err.Error())
			return
		}
		dst, oerr := os.OpenFile(absPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if oerr != nil {
			part.Close()
			abort(http.StatusInternalServerError, "stage file: "+oerr.Error())
			return
		}
		_, cerr := io.Copy(dst, part)
		dst.Close()
		part.Close()
		if cerr != nil {
			abort(http.StatusInternalServerError, "write stage: "+cerr.Error())
			return
		}
		sawAny = true
		// Blocks once the buffer is full — that's the back-pressure.
		pending <- node.FolderFileEntry{AbsPath: absPath, RelPath: rel}
	}
	close(pending)

	if !sawAny {
		<-senderDone
		http.Error(w, "no files in folder upload", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	// sender continues in background; it owns stageDir cleanup.
}

// handleUpload receives files sent from a phone (or any browser) over the LAN
// and writes them straight into the downloads directory — no p2p hop. Multiple
// "file" parts may be sent in one request. On a name clash the new file gets a
// " (n)" suffix so nothing is overwritten. Each saved file is announced over SSE
// so the desktop UI and any open phone pages refresh live.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "expected multipart body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(s.downloadsDir, 0o750); err != nil {
		http.Error(w, "downloads dir: "+err.Error(), http.StatusInternalServerError)
		return
	}

	saved := 0
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, "read part: "+err.Error(), http.StatusBadRequest)
			return
		}
		if part.FormName() != "file" {
			part.Close()
			continue
		}
		name := filepath.Base(part.FileName())
		if name == "." || name == ".." || name == "" {
			part.Close()
			http.Error(w, "invalid filename", http.StatusBadRequest)
			return
		}
		dstPath := uniquePath(s.downloadsDir, name)
		dst, oerr := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
		if oerr != nil {
			part.Close()
			http.Error(w, "create file: "+oerr.Error(), http.StatusInternalServerError)
			return
		}
		_, cerr := io.Copy(dst, part)
		dst.Close()
		part.Close()
		if cerr != nil {
			os.Remove(dstPath)
			http.Error(w, "write file: "+cerr.Error(), http.StatusInternalServerError)
			return
		}
		saved++
		// Announce the new file the same way received p2p files are announced.
		b, _ := json.Marshal(map[string]string{"path": dstPath})
		s.hub.broadcast("file_done", b)
	}

	if saved == 0 {
		http.Error(w, "file field required", http.StatusBadRequest)
		return
	}
	if fl, err := s.fileListJSON(); err == nil {
		s.hub.broadcast("files_changed", fl)
	}
	w.WriteHeader(http.StatusNoContent)
}

// uniquePath returns a path inside dir for name that does not yet exist,
// inserting " (n)" before the extension on collision (e.g. "img (1).jpg").
func uniquePath(dir, name string) string {
	candidate := filepath.Join(dir, name)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func (s *Server) handleFilesList(w http.ResponseWriter, r *http.Request) {
	b, err := s.fileListJSON()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

func (s *Server) handleFileOpen(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.URL.Query().Get("name"))
	if name == "" || name == "." || name == ".." {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	abs := filepath.Join(s.downloadsDir, name)
	// Verify the resolved path is still inside downloadsDir.
	rel, err := filepath.Rel(s.downloadsDir, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(abs); err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	openPath(abs)
	w.WriteHeader(http.StatusNoContent)
}

// handleFileDownload streams a received file's bytes to the client (e.g. a
// phone browser) as an attachment. Range requests are supported via ServeFile,
// so large files and video scrubbing work on mobile.
func (s *Server) handleFileDownload(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.URL.Query().Get("name"))
	if name == "" || name == "." || name == ".." {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	abs := filepath.Join(s.downloadsDir, name)
	// Verify the resolved path is still inside downloadsDir.
	rel, err := filepath.Rel(s.downloadsDir, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	if fi, err := os.Stat(abs); err != nil || fi.IsDir() {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	http.ServeFile(w, r, abs)
}

func (s *Server) handleFileOpenDir(w http.ResponseWriter, r *http.Request) {
	os.MkdirAll(s.downloadsDir, 0o750) //nolint:errcheck
	openPath(s.downloadsDir)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleFoldersList(w http.ResponseWriter, r *http.Request) {
	type folderResp struct {
		Name  string      `json:"name"`
		Files []fileEntry `json:"files"`
	}
	infos := s.node.SharedFolderInfos()
	result := make([]folderResp, 0, len(infos))
	for _, fi := range infos {
		files, _ := s.readDirFiles(fi.Dir)
		result = append(result, folderResp{Name: fi.Name, Files: files})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// openPath opens a file or directory with the OS default handler.
func openPath(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	cmd.Start() //nolint:errcheck
}

