// Package api contains the HTTP layer: handlers, middleware, and
// the router that wires them to the storage backends.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/landmarks-foundation/tours-api/internal/config"
	"github.com/landmarks-foundation/tours-api/internal/storage"
)

// Server holds the dependencies every handler needs.
// Passing this around (rather than package-level globals) makes
// the handlers trivially testable.
type Server struct {
	cfg        *config.Config
	logger     *slog.Logger
	sites      storage.SiteStore
	media      storage.MediaStore
	webRoot    string // optional: directory to serve the HTML admin UI from
	apiHandler http.Handler
}

// NewServer constructs a Server with the given dependencies.
// webRoot may be empty to disable static file serving.
func NewServer(cfg *config.Config, logger *slog.Logger, sites storage.SiteStore, media storage.MediaStore, webRoot string) *Server {
	s := &Server{
		cfg:     cfg,
		logger:  logger,
		sites:   sites,
		media:   media,
		webRoot: webRoot,
	}
	s.apiHandler = s.buildRouter()
	return s
}

// ServeHTTP makes Server satisfy http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.apiHandler.ServeHTTP(w, r)
}

// buildRouter wires routes to handler methods using Go 1.22+ method+path patterns.
func (s *Server) buildRouter() http.Handler {
	mux := http.NewServeMux()

	// --- unauthenticated ---
	mux.HandleFunc("GET /healthz", s.handleHealth)

	// --- sites CRUD (shared-key protected) ---
	sitesMux := http.NewServeMux()
	sitesMux.HandleFunc("GET /api/sites", s.handleListSites)
	sitesMux.HandleFunc("POST /api/sites", s.handleCreateSite)
	sitesMux.HandleFunc("GET /api/sites/{id}", s.handleGetSite)
	sitesMux.HandleFunc("PUT /api/sites/{id}", s.handleUpdateSite)
	sitesMux.HandleFunc("DELETE /api/sites/{id}", s.handleDeleteSite)

	// Bulk operations: matches the existing UI's "Load Sample Data",
	// "Clear All", and "Import JSON".
	sitesMux.HandleFunc("POST /api/sites/bulk/replace", s.handleReplaceAllSites)
	sitesMux.HandleFunc("POST /api/sites/bulk/clear", s.handleClearSites)

	// --- media (shared-key protected) ---
	sitesMux.HandleFunc("POST /api/media", s.handleUploadMedia)
	sitesMux.HandleFunc("GET /api/media/{id}", s.handleGetMedia)
	sitesMux.HandleFunc("DELETE /api/media/{id}", s.handleDeleteMedia)
	sitesMux.HandleFunc("GET /api/media", s.handleListMedia)

	// Attach auth only to /api/*.
	authed := chain(sitesMux,
		requireSharedKey(s.cfg.SharedKey),
	)
	mux.Handle("/api/", authed)

	// --- static UI (optional) ---
	if s.webRoot != "" {
		fs := http.FileServer(http.Dir(s.webRoot))
		// Pattern is "/" (no method) rather than "GET /" so it is
		// strictly less specific than "/api/" on the path dimension —
		// otherwise Go 1.22+'s ServeMux panics at registration because
		// "GET /" matches fewer methods AND a broader path, making
		// neither pattern clearly more specific than "/api/".
		mux.Handle("/", fs)
	}

	// Outer middleware chain: logging + panic recovery + CORS.
	// CORS has to be outermost so it still answers preflight OPTIONS
	// even when downstream returns 401/404.
	return chain(mux,
		logRequests(s.logger),
		recoverPanic(s.logger),
		cors(s.cfg.AllowedOrigins),
	)
}

// --- response helpers ---

// writeJSON serialises v to JSON and sends it with the given status.
// On encoding error it logs but the response is already partially committed
// so we can't switch to an error payload — that's expected behaviour.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// writeError sends a JSON error body: { "error": "<message>" }.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
