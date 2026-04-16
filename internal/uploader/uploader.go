package uploader

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// UploadItem describes a single file to upload.
type UploadItem struct {
	LocalPath string
	SignedURL string
	FilePath  string // relative path within the site
	Size      int64  // file size in bytes
}

// BlobEventType describes the kind of blob progress event.
type BlobEventType int

const (
	BlobStarted  BlobEventType = iota // upload begun
	BlobProgress                      // byte-level progress update
	BlobDone                          // upload complete
)

// BlobEvent reports progress for a single blob upload.
type BlobEvent struct {
	Type     BlobEventType
	Index    int
	FilePath string
	Size     int64
	Uploaded int64
}

// BlobFunc is called with progress events during upload.
type BlobFunc func(BlobEvent)

// countingReader wraps an io.Reader and reports cumulative bytes read.
type countingReader struct {
	reader     io.Reader
	onProgress func(total int64)
	total      int64
	lastReport int64
	threshold  int64 // minimum bytes between reports
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.reader.Read(p)
	cr.total += int64(n)
	if cr.onProgress != nil && (cr.total-cr.lastReport >= cr.threshold || err == io.EOF) {
		cr.lastReport = cr.total
		cr.onProgress(cr.total)
	}
	return n, err
}

// retryableStatus returns true for HTTP status codes that should trigger a retry.
func retryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable:
		return true
	}
	return false
}

// uploadOne uploads a single item to its signed URL with retry logic.
func uploadOne(item UploadItem, httpClient *http.Client, onProgress func(uploaded int64)) error {
	const maxAttempts = 3
	backoff := time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := doUpload(item, httpClient, onProgress)
		if err == nil {
			return nil
		}

		// Only retry on retryable HTTP errors.
		if retryErr, ok := err.(*uploadError); ok && retryableStatus(retryErr.statusCode) {
			if attempt < maxAttempts {
				time.Sleep(backoff)
				backoff *= 2
				continue
			}
		}
		return err
	}
	return nil
}

// uploadError carries an HTTP status code for retry decisions.
type uploadError struct {
	statusCode int
	message    string
}

func (e *uploadError) Error() string {
	return fmt.Sprintf("upload failed with status %d: %s", e.statusCode, e.message)
}

// doUpload performs a single PUT request.
func doUpload(item UploadItem, httpClient *http.Client, onProgress func(uploaded int64)) error {
	f, err := os.Open(item.LocalPath)
	if err != nil {
		return fmt.Errorf("opening %s: %w", item.LocalPath, err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", item.LocalPath, err)
	}

	var body io.Reader = f
	if onProgress != nil {
		body = &countingReader{
			reader:     f,
			onProgress: onProgress,
			threshold:  32 * 1024, // report every 32 KB
		}
	}

	// Use POST for GCS emulator URLs (uploadType=media), PUT for real signed URLs.
	method := http.MethodPut
	if strings.Contains(item.SignedURL, "uploadType=media") {
		method = http.MethodPost
	}
	req, err := http.NewRequest(method, item.SignedURL, body)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.ContentLength = fi.Size()

	ext := filepath.Ext(item.LocalPath)
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return &uploadError{
			statusCode: resp.StatusCode,
			message:    http.StatusText(resp.StatusCode),
		}
	}

	return nil
}

// Upload uploads all items concurrently, bounded by concurrency.
// onBlob, if non-nil, is called with progress events for each blob.
func Upload(items []UploadItem, concurrency int, onBlob BlobFunc) error {
	if len(items) == 0 {
		return nil
	}
	if concurrency < 1 {
		concurrency = 1
	}

	httpClient := &http.Client{
		Timeout: 5 * time.Minute,
	}

	sem := make(chan struct{}, concurrency)
	errs := make(chan error, len(items))

	var wg sync.WaitGroup
	for i, item := range items {
		i, item := i, item
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			if onBlob != nil {
				onBlob(BlobEvent{
					Type:     BlobStarted,
					Index:    i,
					FilePath: item.FilePath,
					Size:     item.Size,
				})
			}

			var progressFn func(int64)
			if onBlob != nil {
				progressFn = func(uploaded int64) {
					onBlob(BlobEvent{
						Type:     BlobProgress,
						Index:    i,
						FilePath: item.FilePath,
						Size:     item.Size,
						Uploaded: uploaded,
					})
				}
			}

			err := uploadOne(item, httpClient, progressFn)
			if err != nil {
				errs <- err
				return
			}

			if onBlob != nil {
				onBlob(BlobEvent{
					Type:     BlobDone,
					Index:    i,
					FilePath: item.FilePath,
					Size:     item.Size,
					Uploaded: item.Size,
				})
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
