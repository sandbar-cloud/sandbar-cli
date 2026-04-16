package hasher

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

type FileEntry struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

// ProgressFunc is called after each file is hashed with the file path,
// number of files completed so far, and total number of files.
type ProgressFunc func(path string, completed, total int)

// HashDir walks dir, computes SHA-256 for each file in parallel.
// Skips hidden files/dirs and .sandbar/.
// If progress is non-nil, it is called after each file is hashed.
func HashDir(dir string, progress ProgressFunc) ([]FileEntry, error) {
	var paths []string

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if rel == "." {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			paths = append(paths, rel)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk dir: %w", err)
	}

	type result struct {
		entry FileEntry
		err   error
	}

	results := make([]result, len(paths))
	total := len(paths)
	concurrency := runtime.NumCPU()
	sem := make(chan struct{}, concurrency)
	var completed int64
	var wg sync.WaitGroup

	for i, rel := range paths {
		wg.Add(1)
		go func(i int, rel string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			fullPath := filepath.Join(dir, rel)
			entry, err := hashFile(fullPath, rel)
			results[i] = result{entry: entry, err: err}

			if progress != nil {
				c := atomic.AddInt64(&completed, 1)
				progress(rel, int(c), total)
			}
		}(i, rel)
	}

	wg.Wait()

	entries := make([]FileEntry, 0, len(results))
	for _, r := range results {
		if r.err != nil {
			return nil, r.err
		}
		entries = append(entries, r.entry)
	}
	return entries, nil
}

func hashFile(fullPath, relPath string) (FileEntry, error) {
	f, err := os.Open(fullPath)
	if err != nil {
		return FileEntry{}, err
	}
	defer f.Close()

	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return FileEntry{}, err
	}

	return FileEntry{
		Path: relPath,
		Hash: fmt.Sprintf("sha256:%x", h.Sum(nil)),
		Size: size,
	}, nil
}
