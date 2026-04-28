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

	n.OnProgress(func(name string, pct float64, recv bool) {
		b, _ := json.Marshal(map[string]any{
			"name": name,
			"pct":  pct,
			"recv": recv,
		})
		s.hub.broadcast("transfer_progress", b)
	})

	return s
}

// ListenAndServe starts the HTTP server on addr (e.g. ":8080"). Blocks.
func (s *Server) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleUI)
	mux.HandleFunc("GET /events", s.handleSSE)
	mux.HandleFunc("GET /state", s.handleState)
	mux.HandleFunc("POST /chat", s.handleChat)
	mux.HandleFunc("POST /file", s.handleFile)
	mux.HandleFunc("GET /files", s.handleFilesList)
	mux.HandleFunc("GET /files/open", s.handleFileOpen)
	mux.HandleFunc("GET /files/opendir", s.handleFileOpenDir)
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

	if err := r.ParseMultipartForm(128 << 20); err != nil {
		http.Error(w, "parse form: "+err.Error(), http.StatusBadRequest)
		return
	}
	f, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file field required", http.StatusBadRequest)
		return
	}
	defer f.Close()

	// Sanitise filename to prevent path traversal.
	name := filepath.Base(header.Filename)
	if name == "." || name == ".." || name == "" {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	dir, err := os.MkdirTemp("", "p2p-upload-*")
	if err != nil {
		http.Error(w, "temp dir: "+err.Error(), http.StatusInternalServerError)
		return
	}

	tmpPath := filepath.Join(dir, name)
	dst, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		os.RemoveAll(dir)
		http.Error(w, "temp file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := io.Copy(dst, f); err != nil {
		dst.Close()
		os.RemoveAll(dir)
		http.Error(w, "write temp: "+err.Error(), http.StatusInternalServerError)
		return
	}
	dst.Close()

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

