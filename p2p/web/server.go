// Package web serves a live browser UI that mirrors the P2P node state.
// It exposes an SSE stream for real-time events and a small REST API for
// sending chats, files, and listing received files.
package web

import (
	"crypto/rand"
	"encoding/hex"
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

// phoneInfo identifies a connected phone (a browser on the /phone page) so the
// desktop UI can show it as a node and target file pushes at it.
type phoneInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// sseClient is one connected browser stream. role is "desktop" (default) or
// "phone"; id/name are only set for phones.
type sseClient struct {
	ch   chan string
	role string
	id   string
	name string
}

// sseHub fans out SSE messages to all connected browser clients, can target a
// single phone, and tracks which phones are currently connected.
type sseHub struct {
	mu      sync.Mutex
	clients map[*sseClient]struct{}
}

func newSSEHub() *sseHub {
	return &sseHub{clients: make(map[*sseClient]struct{})}
}

// subscribe registers a client. role/id/name are retained only so push events
// can be targeted at a specific phone's stream — phone *presence* is tracked
// separately via heartbeats (see Server.phonePresence), not the SSE lifecycle.
func (h *sseHub) subscribe(role, id, name string) *sseClient {
	c := &sseClient{ch: make(chan string, 64), role: role, id: id, name: name}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	return c
}

func (h *sseHub) unsubscribe(c *sseClient) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

func (h *sseHub) broadcast(event string, data []byte) {
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.ch <- msg:
		default: // slow client — drop rather than block
		}
	}
}

// sendToPhone delivers an event only to streams belonging to phone id.
func (h *sseHub) sendToPhone(id, event string, data []byte) {
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.role != "phone" || c.id != id {
			continue
		}
		select {
		case c.ch <- msg:
		default:
		}
	}
}

// pushEntry is a file staged for a single phone to download, keyed by a token.
type pushEntry struct {
	path    string
	name    string
	phoneID string
	created time.Time
}

// Server bridges the Node to a live browser UI.
type Server struct {
	node         *node.Node
	hub          *sseHub
	downloadsDir string

	outboxDir string // temp dir holding files staged for PC→phone download
	pushMu    sync.Mutex
	pushes    map[string]pushEntry // token -> staged file

	phoneMu       sync.Mutex
	phonePresence map[string]*phoneRec // phone id -> last-seen heartbeat
}

// phoneRec tracks a phone that is heartbeating. A phone is considered connected
// while its heartbeats keep arriving; it expires shortly after they stop.
type phoneRec struct {
	name     string
	lastSeen time.Time
}

// Heartbeat timing: phones ping every phonePingInterval; a phone is dropped if
// nothing is heard for phoneTTL (a few missed pings).
const (
	phonePingInterval = 3 * time.Second
	phoneTTL          = 9 * time.Second
)

// New creates a Server wired to n. downloadsDir is the folder that received
// files are written to and listed from. Registers node callbacks immediately;
// call ListenAndServe to start the HTTP server.
func New(n *node.Node, downloadsDir string) *Server {
	s := &Server{
		node:          n,
		hub:           newSSEHub(),
		downloadsDir:  downloadsDir,
		pushes:        make(map[string]pushEntry),
		phonePresence: make(map[string]*phoneRec),
	}

	// Temp dir for files staged to be pushed to a phone. Best-effort: if it
	// can't be created, push requests will fail individually with a clear error.
	if dir, err := os.MkdirTemp("", "p2p-outbox-*"); err == nil {
		s.outboxDir = dir
	}
	go s.sweepPushes()
	go s.sweepPhones()

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
		event := "folder_change"
		if deleted {
			event = "folder_delete"
		}
		b, _ := json.Marshal(map[string]string{"folder": folderName, "rel_path": relPath})
		s.hub.broadcast(event, b)
		// Push the updated file list for this folder.
		if fl, err := s.folderFilesJSON(folderName); err == nil {
			b2, _ := json.Marshal(map[string]any{"folder": folderName, "files": json.RawMessage(fl)})
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
	mux.HandleFunc("POST /push", s.handlePush)
	mux.HandleFunc("GET /pushed", s.handlePushed)
	mux.HandleFunc("POST /phone/ping", s.handlePhonePing)
	mux.HandleFunc("POST /phone/bye", s.handlePhoneBye)
	mux.HandleFunc("GET /phones", s.handlePhonesList)
	mux.HandleFunc("GET /files", s.handleFilesList)
	mux.HandleFunc("GET /files/open", s.handleFileOpen)
	mux.HandleFunc("GET /files/opendir", s.handleFileOpenDir)
	mux.HandleFunc("GET /files/download", s.handleFileDownload)
	mux.HandleFunc("GET /folders", s.handleFoldersList)
	return http.ListenAndServe(addr, mux)
}

// --- snapshot types ---

type selfInfo struct {
	Nick   string `json:"nick"`
	Addr   string `json:"addr"` // this node's peer ID
	Crypto bool   `json:"crypto"`
}

type initState struct {
	Self    selfInfo          `json:"self"`
	Peers   []node.PeerInfo   `json:"peers"`
	Folders []folderStateInfo `json:"folders,omitempty"`
	Phones  []phoneInfo       `json:"phones,omitempty"`
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
			Nick:   s.node.Nick(),
			Addr:   s.node.ID(),
			Crypto: s.node.CryptoEnabled(),
		},
		Peers:   peers,
		Folders: folderStates,
		Phones:  s.phonesList(),
	}
}

// phonesList returns the phones currently heartbeating, sorted by name.
func (s *Server) phonesList() []phoneInfo {
	s.phoneMu.Lock()
	defer s.phoneMu.Unlock()
	out := make([]phoneInfo, 0, len(s.phonePresence))
	for id, rec := range s.phonePresence {
		out = append(out, phoneInfo{ID: id, Name: rec.name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
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

	// Identify the client: phones pass role=phone&id=... so the PC can target
	// file pushes at a specific one. Presence (join/leave) is tracked separately
	// via heartbeats, not this stream's lifecycle.
	role := r.URL.Query().Get("role")
	if role != "phone" {
		role = "desktop"
	}
	id := r.URL.Query().Get("id")
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		name = "phone"
	}

	client := s.hub.subscribe(role, id, name)
	defer s.hub.unsubscribe(client)

	// Keepalive: periodic comment frames keep proxies from closing the idle
	// stream and surface a dead socket (write error) so presence stays live.
	ka := time.NewTicker(15 * time.Second)
	defer ka.Stop()

	for {
		select {
		case msg := <-client.ch:
			if _, err := fmt.Fprint(w, msg); err != nil {
				return
			}
			flusher.Flush()
		case <-ka.C:
			if _, err := fmt.Fprint(w, ":hb\n\n"); err != nil {
				return
			}
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
		err = streamToFile(part, tmpPath, os.O_CREATE|os.O_WRONLY)
		part.Close()
		if err != nil {
			os.RemoveAll(dir)
			http.Error(w, "write temp: "+err.Error(), http.StatusInternalServerError)
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
		err = streamToFile(part, absPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
		part.Close()
		if err != nil {
			abort(http.StatusInternalServerError, "write stage: "+err.Error())
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
		err = streamToFile(part, dstPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL)
		part.Close()
		if err != nil {
			http.Error(w, "write file: "+err.Error(), http.StatusInternalServerError)
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

// handlePush receives a single file from the desktop UI to send to one phone.
// The file is staged in the outbox under a random token and the target phone is
// notified over its SSE stream with a one-time download URL. Folders are not
// supported — phones receive plain files only.
func (s *Server) handlePush(w http.ResponseWriter, r *http.Request) {
	phone := r.URL.Query().Get("phone")
	if phone == "" {
		http.Error(w, "phone query param required", http.StatusBadRequest)
		return
	}
	if s.outboxDir == "" {
		http.Error(w, "outbox unavailable", http.StatusInternalServerError)
		return
	}

	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "expected multipart body: "+err.Error(), http.StatusBadRequest)
		return
	}

	token := randomToken()
	dir := filepath.Join(s.outboxDir, token)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		http.Error(w, "stage dir: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var stagedPath, name string
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
		stagedPath = filepath.Join(dir, name)
		err = streamToFile(part, stagedPath, os.O_CREATE|os.O_WRONLY)
		part.Close()
		if err != nil {
			os.RemoveAll(dir)
			http.Error(w, "write stage: "+err.Error(), http.StatusInternalServerError)
			return
		}
		break
	}
	if stagedPath == "" {
		os.RemoveAll(dir)
		http.Error(w, "file field required", http.StatusBadRequest)
		return
	}

	var size int64
	if fi, err := os.Stat(stagedPath); err == nil {
		size = fi.Size()
	}

	s.pushMu.Lock()
	s.pushes[token] = pushEntry{path: stagedPath, name: name, phoneID: phone, created: time.Now()}
	s.pushMu.Unlock()

	b, _ := json.Marshal(map[string]any{
		"name": name,
		"size": size,
		"url":  "/pushed?token=" + token,
	})
	s.hub.sendToPhone(phone, "push", b)
	w.WriteHeader(http.StatusAccepted)
}

// handlePushed streams a staged push file to the phone, then removes it. The
// token is single-use: once fetched the entry and its temp file are deleted.
func (s *Server) handlePushed(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	s.pushMu.Lock()
	entry, ok := s.pushes[token]
	if ok {
		delete(s.pushes, token)
	}
	s.pushMu.Unlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer os.RemoveAll(filepath.Dir(entry.path))

	if fi, err := os.Stat(entry.path); err != nil || fi.IsDir() {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", entry.name))
	http.ServeFile(w, r, entry.path)
}

// sweepPushes periodically drops staged push files that were never fetched, so
// the outbox doesn't grow without bound when a phone goes away mid-send.
func (s *Server) sweepPushes() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for range t.C {
		cutoff := time.Now().Add(-10 * time.Minute)
		s.pushMu.Lock()
		for token, e := range s.pushes {
			if e.created.Before(cutoff) {
				os.RemoveAll(filepath.Dir(e.path))
				delete(s.pushes, token)
			}
		}
		s.pushMu.Unlock()
	}
}

// handlePhonePing records a heartbeat from a phone. The first ping for an id
// registers the phone (announced via phone_join); later pings keep it alive.
// Phones call this every few seconds; the sweeper drops ones that go silent.
func (s *Server) handlePhonePing(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.ID) == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "phone"
	}

	s.phoneMu.Lock()
	rec, existed := s.phonePresence[req.ID]
	if !existed {
		s.phonePresence[req.ID] = &phoneRec{name: name, lastSeen: time.Now()}
	} else {
		rec.name = name
		rec.lastSeen = time.Now()
	}
	s.phoneMu.Unlock()

	if !existed {
		b, _ := json.Marshal(phoneInfo{ID: req.ID, Name: name})
		s.hub.broadcast("phone_join", b)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePhoneBye lets a phone announce it's leaving (page close) so it drops
// immediately instead of waiting for the heartbeat to expire. Best-effort.
func (s *Server) handlePhoneBye(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.phoneMu.Lock()
	_, existed := s.phonePresence[req.ID]
	delete(s.phonePresence, req.ID)
	s.phoneMu.Unlock()
	if existed {
		b, _ := json.Marshal(map[string]string{"id": req.ID})
		s.hub.broadcast("phone_leave", b)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePhonesList returns the phones currently heartbeating (polled by the
// desktop UI as the authoritative presence list).
func (s *Server) handlePhonesList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.phonesList())
}

// sweepPhones expires phones whose heartbeats have stopped, announcing each
// departure over SSE so the desktop UI removes it promptly.
func (s *Server) sweepPhones() {
	t := time.NewTicker(phonePingInterval)
	defer t.Stop()
	for range t.C {
		var gone []string
		cutoff := time.Now().Add(-phoneTTL)
		s.phoneMu.Lock()
		for id, rec := range s.phonePresence {
			if rec.lastSeen.Before(cutoff) {
				gone = append(gone, id)
				delete(s.phonePresence, id)
			}
		}
		s.phoneMu.Unlock()
		for _, id := range gone {
			b, _ := json.Marshal(map[string]string{"id": id})
			s.hub.broadcast("phone_leave", b)
		}
	}
}

// streamToFile writes src to a new file at path (opened with flags|0o600),
// streaming straight to disk with no in-memory spool. On a copy failure it
// removes the partial file it created; an open failure leaves any existing
// file untouched (important for O_EXCL).
func streamToFile(src io.Reader, path string, flags int) error {
	dst, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(path)
		return err
	}
	return dst.Close()
}

// randomToken returns a 16-byte hex string used to key staged push files.
func randomToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
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

// safeDownloadPath resolves a user-supplied name to an absolute path inside
// downloadsDir, rejecting empty/dot names and anything that escapes the dir.
func (s *Server) safeDownloadPath(name string) (string, bool) {
	name = filepath.Base(name)
	if name == "" || name == "." || name == ".." {
		return "", false
	}
	abs := filepath.Join(s.downloadsDir, name)
	rel, err := filepath.Rel(s.downloadsDir, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return abs, true
}

func (s *Server) handleFileOpen(w http.ResponseWriter, r *http.Request) {
	abs, ok := s.safeDownloadPath(r.URL.Query().Get("name"))
	if !ok {
		http.Error(w, "invalid name", http.StatusBadRequest)
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
	abs, ok := s.safeDownloadPath(r.URL.Query().Get("name"))
	if !ok {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	if fi, err := os.Stat(abs); err != nil || fi.IsDir() {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(abs)))
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

// OpenURL opens a URL in the user's default browser, using the same per-OS
// launcher as openPath (which also works for URLs, not just filesystem paths).
func OpenURL(url string) { openPath(url) }

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
