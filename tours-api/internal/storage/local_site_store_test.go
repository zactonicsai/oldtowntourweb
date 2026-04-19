package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/landmarks-foundation/tours-api/internal/models"
)

// newTestStore returns a LocalSiteStore rooted in t.TempDir so every
// test gets a clean filesystem. t.TempDir is cleaned up automatically
// when the test ends.
func newTestStore(t *testing.T) *LocalSiteStore {
	t.Helper()
	s, err := NewLocalSiteStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalSiteStore: %v", err)
	}
	return s
}

func TestLocalSiteStore_ListEmptyWhenFileMissing(t *testing.T) {
	s := newTestStore(t)
	sites, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List returned error on empty store: %v", err)
	}
	if len(sites) != 0 {
		t.Errorf("expected 0 sites on fresh store, got %d", len(sites))
	}
}

func TestLocalSiteStore_CreateAssignsIDAndTimestamps(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created, err := s.Create(ctx, models.Site{Title: "Lucas Tavern", BeaconID: "B-1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" {
		t.Error("expected Create to assign an ID")
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Error("expected Create to set CreatedAt and UpdatedAt")
	}
	if created.Title != "Lucas Tavern" || created.BeaconID != "B-1" {
		t.Errorf("unexpected content: %+v", created)
	}
}

func TestLocalSiteStore_GetRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created, err := s.Create(ctx, models.Site{Title: "A", BeaconID: "B"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != created.ID || got.Title != "A" {
		t.Errorf("round-trip mismatch: %+v vs %+v", got, created)
	}
}

func TestLocalSiteStore_GetReturnsNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Get(context.Background(), "no-such-id")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestLocalSiteStore_UpdatePreservesCreatedAtRefreshesUpdatedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created, err := s.Create(ctx, models.Site{Title: "Old", BeaconID: "B-1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	originalCreatedAt := created.CreatedAt

	updated, err := s.Update(ctx, created.ID, models.SiteInput{
		Title:    "New",
		BeaconID: "B-1",
		Text:     "More detail",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Title != "New" || updated.Text != "More detail" {
		t.Errorf("Update didn't apply new fields: %+v", updated)
	}
	if !updated.CreatedAt.Equal(originalCreatedAt) {
		t.Errorf("CreatedAt changed: was %v, now %v", originalCreatedAt, updated.CreatedAt)
	}
	if !updated.UpdatedAt.After(originalCreatedAt) && !updated.UpdatedAt.Equal(originalCreatedAt) {
		// Same-nanosecond equality is fine; going backwards isn't.
		if updated.UpdatedAt.Before(originalCreatedAt) {
			t.Errorf("UpdatedAt went backwards: %v < %v", updated.UpdatedAt, originalCreatedAt)
		}
	}
}

func TestLocalSiteStore_UpdateNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Update(context.Background(), "ghost", models.SiteInput{Title: "X", BeaconID: "B"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestLocalSiteStore_Delete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created, err := s.Create(ctx, models.Site{Title: "A", BeaconID: "B"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
	// Second delete of the same id should surface NotFound.
	if err := s.Delete(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound on second delete, got %v", err)
	}
}

func TestLocalSiteStore_ReplaceAllAssignsMissingIDs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := []models.Site{
		{Title: "One", BeaconID: "B-1"}, // no ID
		{ID: "keep-this-id", Title: "Two", BeaconID: "B-2"},
	}
	if err := s.ReplaceAll(ctx, in); err != nil {
		t.Fatalf("ReplaceAll: %v", err)
	}
	out, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 sites, got %d", len(out))
	}
	if out[0].ID == "" {
		t.Error("ReplaceAll should fill in missing IDs")
	}
	if out[1].ID != "keep-this-id" {
		t.Errorf("ReplaceAll should preserve supplied IDs, got %q", out[1].ID)
	}
}

func TestLocalSiteStore_Clear(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := s.Create(ctx, models.Site{Title: "x", BeaconID: "b"}); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	if err := s.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	sites, _ := s.List(ctx)
	if len(sites) != 0 {
		t.Errorf("expected 0 sites after Clear, got %d", len(sites))
	}
}

func TestLocalSiteStore_PersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()

	s1, err := NewLocalSiteStore(dir)
	if err != nil {
		t.Fatalf("NewLocalSiteStore: %v", err)
	}
	created, err := s1.Create(context.Background(), models.Site{Title: "Persist", BeaconID: "P"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Second store instance pointed at the same directory should see
	// the record.
	s2, err := NewLocalSiteStore(dir)
	if err != nil {
		t.Fatalf("NewLocalSiteStore (reopen): %v", err)
	}
	got, err := s2.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if got.Title != "Persist" {
		t.Errorf("expected title 'Persist' after reopen, got %q", got.Title)
	}
}

func TestLocalSiteStore_AtomicWriteLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalSiteStore(dir)
	_, _ = s.Create(context.Background(), models.Site{Title: "x", BeaconID: "b"})

	tmp := filepath.Join(dir, "sites.json.tmp")
	if _, err := os.Stat(tmp); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected %s to be gone after write, got err=%v", tmp, err)
	}
}
