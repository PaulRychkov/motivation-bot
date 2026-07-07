package prompts

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"
)

func TestGetFallbackWhenNoFile(t *testing.T) {
	s := New(t.TempDir(), nil, map[string]string{"dialog": "фолбэк"})
	if got := s.Get("dialog"); got != "фолбэк" {
		t.Errorf("Get = %q", got)
	}
}

func TestGetReadsDiskAndHotReloads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dialog.md")
	if err := os.WriteFile(path, []byte("первая версия"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, map[string]string{"dialog": "фолбэк"})
	if got := s.Get("dialog"); got != "первая версия" {
		t.Fatalf("Get = %q", got)
	}
	newTime := time.Now().Add(2 * time.Second)
	if err := os.WriteFile(path, []byte("вторая версия — длиннее"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, newTime, newTime); err != nil {
		t.Fatal(err)
	}
	if got := s.Get("dialog"); got != "вторая версия — длиннее" {
		t.Errorf("hot-reload не сработал: %q", got)
	}
}

func TestGetEmptyFileFallsBack(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dialog.md"), []byte("   \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, map[string]string{"dialog": "фолбэк"})
	if got := s.Get("dialog"); got != "фолбэк" {
		t.Errorf("пустой файл не должен затирать промпт: %q", got)
	}
}

func TestGetEmbeddedWhenNoDiskFile(t *testing.T) {
	embedded := fstest.MapFS{
		"dialog.md": &fstest.MapFile{Data: []byte("встроенный промпт\n")},
	}
	s := New(t.TempDir(), embedded, map[string]string{"dialog": "фолбэк"})
	if got := s.Get("dialog"); got != "встроенный промпт" {
		t.Errorf("Get = %q", got)
	}
}

func TestGetDiskOverridesEmbedded(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dialog.md"), []byte("с диска"), 0o644); err != nil {
		t.Fatal(err)
	}
	embedded := fstest.MapFS{
		"dialog.md": &fstest.MapFile{Data: []byte("встроенный промпт")},
	}
	s := New(dir, embedded, nil)
	if got := s.Get("dialog"); got != "с диска" {
		t.Errorf("Get = %q", got)
	}
}
