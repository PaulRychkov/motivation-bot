package prompts

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Store struct {
	dir       string
	embedded  fs.FS
	fallbacks map[string]string
	mu        sync.Mutex
	cache     map[string]entry
}

type entry struct {
	text    string
	modTime time.Time
	size    int64
}

func New(dir string, embedded fs.FS, fallbacks map[string]string) *Store {
	return &Store{dir: dir, embedded: embedded, fallbacks: fallbacks, cache: map[string]entry{}}
}

func (s *Store) Get(name string) string {
	if text, ok := s.fromDisk(name); ok {
		return text
	}
	if text, ok := s.fromEmbedded(name); ok {
		return text
	}
	return s.fallbacks[name]
}

func (s *Store) fromDisk(name string) (string, bool) {
	path := filepath.Join(s.dir, name+".md")
	fi, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	s.mu.Lock()
	cached, ok := s.cache[name]
	s.mu.Unlock()
	if ok && cached.modTime.Equal(fi.ModTime()) && cached.size == fi.Size() {
		return cached.text, true
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	text := strings.TrimSpace(string(b))
	if text == "" {
		return "", false
	}
	s.mu.Lock()
	s.cache[name] = entry{text: text, modTime: fi.ModTime(), size: fi.Size()}
	s.mu.Unlock()
	return text, true
}

func (s *Store) fromEmbedded(name string) (string, bool) {
	if s.embedded == nil {
		return "", false
	}
	b, err := fs.ReadFile(s.embedded, name+".md")
	if err != nil {
		return "", false
	}
	text := strings.TrimSpace(string(b))
	return text, text != ""
}
