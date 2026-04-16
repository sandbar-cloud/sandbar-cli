package hasher

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func sha256OfBytes(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

func TestHashDir(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "style.css"), []byte("body { color: red; }"), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := HashDir(dir, nil)
	if err != nil {
		t.Fatalf("HashDir returned error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	hashRe := regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	for _, e := range entries {
		if !hashRe.MatchString(e.Hash) {
			t.Errorf("hash %q does not match expected format sha256:<64 hex chars>", e.Hash)
		}
		if e.Size <= 0 {
			t.Errorf("entry %q has non-positive size %d", e.Path, e.Size)
		}
	}
}

func TestHashDir_SkipsHiddenAndSandbar(t *testing.T) {
	dir := t.TempDir()

	// Visible file — should be included
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	// Hidden file — should be skipped
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	// .sandbar directory — should be skipped
	if err := os.MkdirAll(filepath.Join(dir, ".sandbar"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".sandbar", "manifest.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	// .git directory — should be skipped
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("[core]"), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := HashDir(dir, nil)
	if err != nil {
		t.Fatalf("HashDir returned error: %v", err)
	}

	if len(entries) != 1 {
		paths := make([]string, len(entries))
		for i, e := range entries {
			paths[i] = e.Path
		}
		t.Fatalf("expected 1 entry (index.html), got %d: %v", len(entries), paths)
	}

	if entries[0].Path != "index.html" {
		t.Errorf("expected path index.html, got %q", entries[0].Path)
	}
}

func TestHashDir_DeterministicHash(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	content := []byte("deterministic content")

	if err := os.WriteFile(filepath.Join(dir1, "file.txt"), content, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "file.txt"), content, 0644); err != nil {
		t.Fatal(err)
	}

	entries1, err := HashDir(dir1, nil)
	if err != nil {
		t.Fatalf("HashDir(dir1) error: %v", err)
	}
	entries2, err := HashDir(dir2, nil)
	if err != nil {
		t.Fatalf("HashDir(dir2) error: %v", err)
	}

	if len(entries1) != 1 || len(entries2) != 1 {
		t.Fatalf("expected 1 entry each, got %d and %d", len(entries1), len(entries2))
	}

	if entries1[0].Hash != entries2[0].Hash {
		t.Errorf("hashes differ for same content: %q vs %q", entries1[0].Hash, entries2[0].Hash)
	}

	// Known SHA-256 of "deterministic content"
	want := fmt.Sprintf("sha256:%x", sha256OfBytes(content))
	if entries1[0].Hash != want {
		t.Errorf("hash mismatch: got %q, want %q", entries1[0].Hash, want)
	}
}

func TestHashDir_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	entries, err := HashDir(dir, nil)
	if err != nil {
		t.Fatalf("HashDir returned error: %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("expected empty slice, got %d entries", len(entries))
	}
}
