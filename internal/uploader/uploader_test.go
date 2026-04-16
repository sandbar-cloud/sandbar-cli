package uploader

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// makeTestFile creates a temp file with the given content and returns its path and size.
func makeTestFile(t *testing.T, name, content string) (string, int64) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	data := []byte(content)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path, int64(len(data))
}

func TestUpload_SendsFiles(t *testing.T) {
	var received int32
	methods := make(chan string, 2)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods <- r.Method
		io.Copy(io.Discard, r.Body)
		atomic.AddInt32(&received, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	file1, size1 := makeTestFile(t, "index.html", "<html></html>")
	file2, size2 := makeTestFile(t, "style.css", "body{}")

	items := []UploadItem{
		{LocalPath: file1, SignedURL: server.URL + "/upload1", FilePath: "index.html", Size: size1},
		{LocalPath: file2, SignedURL: server.URL + "/upload2", FilePath: "style.css", Size: size2},
	}

	err := Upload(items, 2, nil)
	if err != nil {
		t.Fatalf("Upload returned error: %v", err)
	}

	close(methods)
	for method := range methods {
		if method != http.MethodPut {
			t.Errorf("expected PUT, got %s", method)
		}
	}

	if got := atomic.LoadInt32(&received); got != 2 {
		t.Errorf("expected 2 requests, got %d", got)
	}
}

func TestUpload_Progress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	file1, size1 := makeTestFile(t, "a.txt", "hello")
	file2, size2 := makeTestFile(t, "b.txt", "world")

	items := []UploadItem{
		{LocalPath: file1, SignedURL: server.URL + "/a", FilePath: "a.txt", Size: size1},
		{LocalPath: file2, SignedURL: server.URL + "/b", FilePath: "b.txt", Size: size2},
	}

	var startCount, doneCount int32
	onBlob := func(e BlobEvent) {
		switch e.Type {
		case BlobStarted:
			atomic.AddInt32(&startCount, 1)
		case BlobDone:
			atomic.AddInt32(&doneCount, 1)
		}
	}

	err := Upload(items, 2, onBlob)
	if err != nil {
		t.Fatalf("Upload returned error: %v", err)
	}

	if got := atomic.LoadInt32(&startCount); got != int32(len(items)) {
		t.Errorf("started %d times, want %d", got, len(items))
	}
	if got := atomic.LoadInt32(&doneCount); got != int32(len(items)) {
		t.Errorf("done %d times, want %d", got, len(items))
	}
}

func TestUpload_RetryOn500(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	file, size := makeTestFile(t, "index.html", "<html></html>")

	items := []UploadItem{
		{LocalPath: file, SignedURL: server.URL + "/upload", FilePath: "index.html", Size: size},
	}

	err := Upload(items, 1, nil)
	if err != nil {
		t.Fatalf("Upload returned error: %v", err)
	}

	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}
