// Package storage defines the abstract interfaces for persisting site
// records and media blobs, plus concrete implementations.
//
// The two interfaces (SiteStore, MediaStore) isolate the HTTP layer
// from the actual storage backend. Today we ship LocalSiteStore and
// LocalMediaStore which use the filesystem. In the future, an
// AzureMediaStore can be added that implements MediaStore using
// the Azure Blob Storage SDK (github.com/Azure/azure-sdk-for-go/sdk/storage/azblob)
// without any change to the API handlers — wire it up in main.go.
package storage

import (
	"context"
	"errors"
	"io"

	"github.com/landmarks-foundation/tours-api/internal/models"
)

// ErrNotFound is returned by stores when the requested item doesn't
// exist. HTTP handlers map this to a 404.
var ErrNotFound = errors.New("not found")

// SiteStore is the contract for persisting Site records.
// All methods must be safe for concurrent use.
type SiteStore interface {
	List(ctx context.Context) ([]models.Site, error)
	Get(ctx context.Context, id string) (*models.Site, error)
	Create(ctx context.Context, s models.Site) (*models.Site, error)
	Update(ctx context.Context, id string, in models.SiteInput) (*models.Site, error)
	Delete(ctx context.Context, id string) error
	ReplaceAll(ctx context.Context, sites []models.Site) error
	Clear(ctx context.Context) error
}

// MediaStore is the contract for persisting uploaded media blobs.
// Implementations may be local disk, Azure Blob, S3, etc.
//
// Save reads from r and stores the bytes. It returns a MediaObject
// whose URL field is what the client should persist in a site record.
// For the local backend that URL is a relative path like
// "/api/media/<id>" which the same server serves back via Open.
type MediaStore interface {
	Save(ctx context.Context, filename, contentType string, r io.Reader) (*models.MediaObject, error)
	Open(ctx context.Context, id string) (io.ReadCloser, *models.MediaObject, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]models.MediaObject, error)
	Clear(ctx context.Context) error
}
