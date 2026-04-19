package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/landmarks-foundation/tours-api/internal/models"
)

// LocalSiteStore persists the full list of sites as a single JSON
// file (sites.json) inside the configured data directory.
//
// A single file is sufficient for the ~dozens of tour stops this app
// is sized for. All operations load / rewrite the whole file under
// a mutex. If the scale ever grows, swap this out for a real DB-backed
// implementation of SiteStore.
type LocalSiteStore struct {
	path string
	mu   sync.Mutex
}

// NewLocalSiteStore creates the data directory if needed and returns
// a ready-to-use store.
func NewLocalSiteStore(dataDir string) (*LocalSiteStore, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	return &LocalSiteStore{path: filepath.Join(dataDir, "sites.json")}, nil
}

// List returns every site. Result order is stable (insertion order).
func (s *LocalSiteStore) List(_ context.Context) ([]models.Site, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked()
}

func (s *LocalSiteStore) Get(_ context.Context, id string) (*models.Site, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sites, err := s.readLocked()
	if err != nil {
		return nil, err
	}
	for i := range sites {
		if sites[i].ID == id {
			return &sites[i], nil
		}
	}
	return nil, ErrNotFound
}

func (s *LocalSiteStore) Create(_ context.Context, site models.Site) (*models.Site, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sites, err := s.readLocked()
	if err != nil {
		return nil, err
	}
	if site.ID == "" {
		site.ID = newID()
	}
	now := time.Now().UTC()
	if site.CreatedAt.IsZero() {
		site.CreatedAt = now
	}
	site.UpdatedAt = now
	sites = append(sites, site)
	if err := s.writeLocked(sites); err != nil {
		return nil, err
	}
	return &site, nil
}

func (s *LocalSiteStore) Update(_ context.Context, id string, in models.SiteInput) (*models.Site, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sites, err := s.readLocked()
	if err != nil {
		return nil, err
	}
	for i := range sites {
		if sites[i].ID == id {
			sites[i].Title = in.Title
			sites[i].BeaconID = in.BeaconID
			sites[i].Text = in.Text
			sites[i].AudioURL = in.AudioURL
			sites[i].VideoURL = in.VideoURL
			sites[i].UpdatedAt = time.Now().UTC()
			if err := s.writeLocked(sites); err != nil {
				return nil, err
			}
			out := sites[i]
			return &out, nil
		}
	}
	return nil, ErrNotFound
}

func (s *LocalSiteStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sites, err := s.readLocked()
	if err != nil {
		return err
	}
	found := false
	out := sites[:0]
	for _, site := range sites {
		if site.ID == id {
			found = true
			continue
		}
		out = append(out, site)
	}
	if !found {
		return ErrNotFound
	}
	return s.writeLocked(out)
}

func (s *LocalSiteStore) ReplaceAll(_ context.Context, sites []models.Site) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for i := range sites {
		if sites[i].ID == "" {
			sites[i].ID = newID()
		}
		if sites[i].CreatedAt.IsZero() {
			sites[i].CreatedAt = now
		}
		if sites[i].UpdatedAt.IsZero() {
			sites[i].UpdatedAt = now
		}
	}
	return s.writeLocked(sites)
}

func (s *LocalSiteStore) Clear(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeLocked([]models.Site{})
}

// --- internal, caller must hold s.mu ---

func (s *LocalSiteStore) readLocked() ([]models.Site, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []models.Site{}, nil
		}
		return nil, fmt.Errorf("read sites file: %w", err)
	}
	if len(data) == 0 {
		return []models.Site{}, nil
	}
	var sites []models.Site
	if err := json.Unmarshal(data, &sites); err != nil {
		return nil, fmt.Errorf("decode sites file: %w", err)
	}
	return sites, nil
}

func (s *LocalSiteStore) writeLocked(sites []models.Site) error {
	data, err := json.MarshalIndent(sites, "", "  ")
	if err != nil {
		return fmt.Errorf("encode sites: %w", err)
	}
	// Atomic write: write to tmp then rename, so a crash mid-write
	// can't leave a partially-written sites.json on disk.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp sites file: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("rename sites file: %w", err)
	}
	return nil
}

// newID returns a short random hex identifier. 16 bytes = 128 bits,
// collision probability is effectively zero for this dataset size.
func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
