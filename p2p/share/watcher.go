// Package share provides a polling-based directory watcher used by the shared
// folder feature. It emits Change events when files are added, modified, or
// deleted within a watched directory tree.
package share

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ChangeKind identifies the kind of file system event.
type ChangeKind int

const (
	Added    ChangeKind = iota
	Modified            // size or mtime changed
	Deleted
)

// Change describes a single file event within a watched folder.
type Change struct {
	Kind    ChangeKind
	RelPath string // forward-slash path relative to the folder root
	AbsPath string // absolute path; empty for Deleted
	ModTime int64  // unix seconds; 0 for Deleted
}

type fileState struct {
	size    int64
	modTime int64
}

// Watcher polls a directory every 2 s and emits Changes on its channel.
type Watcher struct {
	Name    string
	Dir     string
	Changes chan Change

	mu    sync.Mutex
	known map[string]fileState // relPath → state
	quit  chan struct{}
}

// New creates a Watcher for the named shared folder at dir.
func New(name, dir string) *Watcher {
	return &Watcher{
		Name:    name,
		Dir:     dir,
		Changes: make(chan Change, 64),
		known:   make(map[string]fileState),
		quit:    make(chan struct{}),
	}
}

// Start creates the folder if needed, snapshots the initial state (without
// emitting events), then begins polling every 2 s.
func (w *Watcher) Start() error {
	if err := os.MkdirAll(w.Dir, 0o750); err != nil {
		return err
	}
	// Populate initial known state so first poll doesn't flood with Added events.
	current := w.walk()
	w.mu.Lock()
	for k, v := range current {
		w.known[k] = v
	}
	w.mu.Unlock()

	go w.loop()
	return nil
}

// Close stops the polling goroutine.
func (w *Watcher) Close() {
	close(w.quit)
}

// Refresh updates the watcher's record for a single file so the next poll
// does not emit a spurious event. Call this after writing a remotely-received
// file into the folder.
func (w *Watcher) Refresh(relPath string) {
	abs := filepath.Join(w.Dir, filepath.FromSlash(relPath))
	info, err := os.Stat(abs)
	if err != nil {
		return
	}
	w.mu.Lock()
	w.known[relPath] = fileState{size: info.Size(), modTime: info.ModTime().Unix()}
	w.mu.Unlock()
}

// RefreshDelete removes a file from the watcher's record so the next poll
// does not re-emit a Deleted event. Call this after applying a remote delete.
func (w *Watcher) RefreshDelete(relPath string) {
	w.mu.Lock()
	delete(w.known, relPath)
	w.mu.Unlock()
}

func (w *Watcher) loop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-w.quit:
			return
		case <-ticker.C:
			w.scanEmit()
		}
	}
}

func (w *Watcher) scanEmit() {
	current := w.walk()

	w.mu.Lock()
	defer w.mu.Unlock()

	// Added or modified.
	for rel, st := range current {
		old, exists := w.known[rel]
		if !exists {
			abs := filepath.Join(w.Dir, filepath.FromSlash(rel))
			select {
			case w.Changes <- Change{Kind: Added, RelPath: rel, AbsPath: abs, ModTime: st.modTime}:
			default:
			}
		} else if st.modTime != old.modTime || st.size != old.size {
			abs := filepath.Join(w.Dir, filepath.FromSlash(rel))
			select {
			case w.Changes <- Change{Kind: Modified, RelPath: rel, AbsPath: abs, ModTime: st.modTime}:
			default:
			}
		}
	}

	// Deleted.
	for rel := range w.known {
		if _, exists := current[rel]; !exists {
			select {
			case w.Changes <- Change{Kind: Deleted, RelPath: rel}:
			default:
			}
		}
	}

	w.known = current
}

// walk returns a snapshot of all regular files under w.Dir.
func (w *Watcher) walk() map[string]fileState {
	result := make(map[string]fileState)
	filepath.WalkDir(w.Dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(w.Dir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return nil
		}
		result[rel] = fileState{size: info.Size(), modTime: info.ModTime().Unix()}
		return nil
	})
	return result
}
