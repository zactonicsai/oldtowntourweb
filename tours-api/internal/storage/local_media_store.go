package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/landmarks-foundation/tours-api/internal/models"
)

// LocalMediaStore stores each uploaded blob as two files in
// <dataDir>/media/:
//
//	<id>.bin    raw file bytes
//	<id>.json   metadata (filename, content-type, size, uploadedAt)
//
// The public URL returned from Save is a relative path
// "/api/media/<id>" that the same server serves back through Open.
//
// Azure swap notes (future AzureMediaStore):
//   - Save:   upload to a container via azblob.BlockBlobClient.UploadStream,
//             return URL pointing to the blob (public or SAS-signed).
//   - Open:   return a ReadCloser over azblob.BlockBlobClient.DownloadStream.
//   - Delete: azblob.BlockBlobClient.Delete.
//   - List:   container.NewListBlobsFlatPager.
//
// Because the HTTP handlers only depend on the MediaStore interface,
// switching backends is a one-liner in main.go.
type LocalMediaStore struct {
	dir string
	mu  sync.Mutex
}

// NewLocalMediaStore creates <dataDir>/media and returns a ready store.
func NewLocalMediaStore(dataDir string) (*LocalMediaStore, error) {
	dir := filepath.Join(dataDir, "media")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create media dir: %w", err)
	}
	return &LocalMediaStore{dir: dir}, nil
}

// Save streams r to a new blob on disk and records metadata alongside.
// The returned MediaObject.URL is what callers should persist on a Site.
func (m *LocalMediaStore) Save(_ context.Context, filename, contentType string, r io.Reader) (*models.MediaObject, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := newID()
	blobPath := filepath.Join(m.dir, id+".bin")
	metaPath := filepath.Join(m.dir, id+".json")

	// Write blob first.
	f, err := os.Create(blobPath)
	if err != nil {
		return nil, fmt.Errorf("create blob: %w", err)
	}
	size, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(blobPath) // best-effort cleanup
		return nil, fmt.Errorf("write blob: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(blobPath)
		return nil, fmt.Errorf("close blob: %w", closeErr)
	}

	// Default filename if client didn't supply one.
	if strings.TrimSpace(filename) == "" {
		filename = id + ".bin"
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/octet-stream"
	}

	meta := &models.MediaObject{
		ID:          id,
		URL:         "/api/media/" + id,
		Filename:    filename,
		ContentType: contentType,
		Size:        size,
		UploadedAt:  time.Now().UTC(),
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		_ = os.Remove(blobPath)
		return nil, fmt.Errorf("encode media meta: %w", err)
	}
	if err := os.WriteFile(metaPath, metaBytes, 0o644); err != nil {
		_ = os.Remove(blobPath)
		return nil, fmt.Errorf("write media meta: %w", err)
	}
	return meta, nil
}

// Open returns a reader over the blob's bytes plus its metadata.
// Caller must Close the reader.
func (m *LocalMediaStore) Open(_ context.Context, id string) (io.ReadCloser, *models.MediaObject, error) {
	if !isSafeID(id) {
		return nil, nil, ErrNotFound
	}
	meta, err := m.loadMeta(id)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(filepath.Join(m.dir, id+".bin"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("open blob: %w", err)
	}
	return f, meta, nil
}

func (m *LocalMediaStore) Delete(_ context.Context, id string) error {
	if !isSafeID(id) {
		return ErrNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	blobPath := filepath.Join(m.dir, id+".bin")
	metaPath := filepath.Join(m.dir, id+".json")

	blobErr := os.Remove(blobPath)
	metaErr := os.Remove(metaPath)

	// If neither existed, surface a NotFound.
	if errors.Is(blobErr, fs.ErrNotExist) && errors.Is(metaErr, fs.ErrNotExist) {
		return ErrNotFound
	}
	// Otherwise we've done the best-effort cleanup; ignore "already gone".
	if blobErr != nil && !errors.Is(blobErr, fs.ErrNotExist) {
		return fmt.Errorf("remove blob: %w", blobErr)
	}
	if metaErr != nil && !errors.Is(metaErr, fs.ErrNotExist) {
		return fmt.Errorf("remove meta: %w", metaErr)
	}
	return nil
}

func (m *LocalMediaStore) List(_ context.Context) ([]models.MediaObject, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, fmt.Errorf("read media dir: %w", err)
	}
	var out []models.MediaObject
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		if !isSafeID(id) {
			continue
		}
		meta, err := m.loadMeta(id)
		if err != nil {
			continue // skip unreadable
		}
		out = append(out, *meta)
	}
	return out, nil
}

func (m *LocalMediaStore) Clear(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return fmt.Errorf("read media dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		_ = os.Remove(filepath.Join(m.dir, e.Name()))
	}
	return nil
}

func (m *LocalMediaStore) loadMeta(id string) (*models.MediaObject, error) {
	data, err := os.ReadFile(filepath.Join(m.dir, id+".json"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read media meta: %w", err)
	}
	var meta models.MediaObject
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("decode media meta: %w", err)
	}
	return &meta, nil
}

// isSafeID guards against path traversal on the id path segment.
// IDs are generated by newID() as 32 hex chars, so we accept only that.
func isSafeID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
