package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/landmarks-foundation/tours-api/internal/models"
	"github.com/landmarks-foundation/tours-api/internal/storage"
)

// GET /healthz — unauthenticated liveness probe.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GET /api/sites — return every site as a JSON array.
func (s *Server) handleListSites(w http.ResponseWriter, r *http.Request) {
	sites, err := s.sites.List(r.Context())
	if err != nil {
		s.logger.Error("list sites", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to list sites")
		return
	}
	if sites == nil {
		sites = []models.Site{}
	}
	writeJSON(w, http.StatusOK, sites)
}

// GET /api/sites/{id} — return a single site.
func (s *Server) handleGetSite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	site, err := s.sites.Get(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "site not found")
		return
	}
	if err != nil {
		s.logger.Error("get site", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to get site")
		return
	}
	writeJSON(w, http.StatusOK, site)
}

// POST /api/sites — create a new site from a SiteInput JSON body.
func (s *Server) handleCreateSite(w http.ResponseWriter, r *http.Request) {
	in, err := decodeSiteInput(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	site := models.Site{
		Title:    in.Title,
		BeaconID: in.BeaconID,
		Text:     in.Text,
		AudioURL: in.AudioURL,
		VideoURL: in.VideoURL,
	}
	created, err := s.sites.Create(r.Context(), site)
	if err != nil {
		s.logger.Error("create site", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to create site")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// PUT /api/sites/{id} — replace an existing site's fields.
func (s *Server) handleUpdateSite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	in, err := decodeSiteInput(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.sites.Update(r.Context(), id, in)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "site not found")
		return
	}
	if err != nil {
		s.logger.Error("update site", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to update site")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// DELETE /api/sites/{id} — remove a site.
//
// NOTE: the linked media blobs are NOT auto-removed here because the
// browser UI may have already uploaded / referenced them independently.
// The UI calls DELETE /api/media/{id} explicitly after DELETE site.
func (s *Server) handleDeleteSite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.sites.Delete(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "site not found")
		return
	}
	if err != nil {
		s.logger.Error("delete site", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to delete site")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/sites/bulk/replace — replace the entire collection.
// Body: { "sites": [ ...Site... ] }
// Used by the "Load Sample Data" and "Import JSON" flows.
func (s *Server) handleReplaceAllSites(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Sites []models.Site `json:"sites"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if err := s.sites.ReplaceAll(r.Context(), body.Sites); err != nil {
		s.logger.Error("replace all sites", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to replace sites")
		return
	}
	// Return the fresh list so the UI can render without a second round-trip.
	out, err := s.sites.List(r.Context())
	if err != nil {
		s.logger.Error("list after replace", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to read sites after replace")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/sites/bulk/clear — delete all sites. Does not touch media.
func (s *Server) handleClearSites(w http.ResponseWriter, r *http.Request) {
	if err := s.sites.Clear(r.Context()); err != nil {
		s.logger.Error("clear sites", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to clear sites")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decodeSiteInput parses + validates a SiteInput body.
func decodeSiteInput(r *http.Request) (models.SiteInput, error) {
	var in models.SiteInput
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return in, errors.New("invalid JSON body: " + err.Error())
	}
	in.Title = strings.TrimSpace(in.Title)
	in.BeaconID = strings.TrimSpace(in.BeaconID)
	in.Text = strings.TrimSpace(in.Text)
	in.AudioURL = strings.TrimSpace(in.AudioURL)
	in.VideoURL = strings.TrimSpace(in.VideoURL)

	if in.Title == "" {
		return in, errors.New("title is required")
	}
	if in.BeaconID == "" {
		return in, errors.New("beaconId is required")
	}
	return in, nil
}
