// Package models contains the domain types for the tour-sites API.
package models

import "time"

// Site represents a single self-guided tour site (a stop on the tour).
// It is serialised to/from JSON by the HTTP layer and persisted by
// whichever storage backend is wired up.
type Site struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	BeaconID  string    `json:"beaconId"`
	Text      string    `json:"text"`
	AudioURL  string    `json:"audioUrl"`
	VideoURL  string    `json:"videoUrl"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// SiteInput is the payload accepted on create / update. Server-side
// fields (ID, timestamps) are NOT read from the client.
type SiteInput struct {
	Title    string `json:"title"`
	BeaconID string `json:"beaconId"`
	Text     string `json:"text"`
	AudioURL string `json:"audioUrl"`
	VideoURL string `json:"videoUrl"`
}

// MediaObject is the metadata returned after a successful upload.
// URL is the value the client should store in Site.AudioURL /
// Site.VideoURL and later GET to play back.
type MediaObject struct {
	ID          string    `json:"id"`
	URL         string    `json:"url"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"size"`
	UploadedAt  time.Time `json:"uploadedAt"`
}
