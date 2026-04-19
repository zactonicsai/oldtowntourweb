package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/landmarks-foundation/tours-api/internal/storage"
)

// POST /api/media — upload a file.
//
// Two request shapes are accepted:
//
//  1. multipart/form-data, field name "file" (what the HTML UI sends)
//  2. raw bytes: Content-Type: <the file's type>, body = bytes.
//     Filename optional via ?filename=foo.mp3 query param.
//
// Response: 201 with the full MediaObject (JSON). The "url" field is
// what the client writes into Site.AudioURL / Site.VideoURL.
func (s *Server) handleUploadMedia(w http.ResponseWriter, r *http.Request) {
	// Enforce max upload size early. http.MaxBytesReader returns
	// a reader that short-circuits once the limit is exceeded.
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadBytes)

	ctype := r.Header.Get("Content-Type")

	var (
		filename    string
		contentType string
		reader      io.Reader
	)

	switch {
	case startsWith(ctype, "multipart/form-data"):
		if err := r.ParseMultipartForm(s.cfg.MaxUploadBytes); err != nil {
			// MaxBytesReader short-circuits mid-parse with MaxBytesError;
			// surface that as 413 instead of a generic 400.
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				writeError(w, http.StatusRequestEntityTooLarge,
					"file exceeds maximum upload size of "+strconv.FormatInt(s.cfg.MaxUploadBytes, 10)+" bytes")
				return
			}
			writeError(w, http.StatusBadRequest, "invalid multipart upload: "+err.Error())
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "missing 'file' form field")
			return
		}
		defer file.Close()
		filename = header.Filename
		contentType = header.Header.Get("Content-Type")
		reader = file

	default:
		// Raw-body upload. Content-Type is whatever the client sent.
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		filename = r.URL.Query().Get("filename")
		contentType = ctype
		reader = r.Body
		defer r.Body.Close()
	}

	meta, err := s.media.Save(r.Context(), filename, contentType, reader)
	if err != nil {
		// MaxBytesReader surfaces as *http.MaxBytesError on newer Go.
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge,
				"file exceeds maximum upload size of "+strconv.FormatInt(s.cfg.MaxUploadBytes, 10)+" bytes")
			return
		}
		s.logger.Error("save media", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to save media")
		return
	}
	writeJSON(w, http.StatusCreated, meta)
}

// GET /api/media/{id} — stream the blob back with its stored content-type.
//
// Because <audio src> and <video src> can't set custom headers,
// the auth middleware also accepts ?key=... on this route.
func (s *Server) handleGetMedia(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rc, meta, err := s.media.Open(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "media not found")
		return
	}
	if err != nil {
		s.logger.Error("open media", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to open media")
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", meta.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(meta.Size, 10))
	// Let <audio>/<video> scrub / seek in the file.
	w.Header().Set("Accept-Ranges", "bytes")
	// Cache-Control: media is immutable once uploaded (new upload = new id).
	w.Header().Set("Cache-Control", "private, max-age=3600")

	if _, err := io.Copy(w, rc); err != nil {
		// Client disconnect etc. — already streaming, just log.
		s.logger.Debug("stream media aborted", "id", id, "err", err)
	}
}

// DELETE /api/media/{id} — remove a blob + its metadata.
func (s *Server) handleDeleteMedia(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.media.Delete(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "media not found")
		return
	}
	if err != nil {
		s.logger.Error("delete media", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to delete media")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/media — list all media objects (metadata only, no bytes).
// Used by the export flow.
func (s *Server) handleListMedia(w http.ResponseWriter, r *http.Request) {
	items, err := s.media.List(r.Context())
	if err != nil {
		s.logger.Error("list media", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to list media")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// startsWith is a tiny helper that avoids importing strings just for HasPrefix.
func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
