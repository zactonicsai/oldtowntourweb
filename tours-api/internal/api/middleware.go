package api

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"runtime/debug"
	"slices"
	"strings"
	"time"
)

// Middleware is a standard http.Handler decorator.
type Middleware func(http.Handler) http.Handler

// chain applies middleware right-to-left so the first mw listed
// runs first on the request path.
func chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// --- auth ---

// requireSharedKey checks the X-API-Key header against the
// configured shared secret using constant-time comparison.
//
// The key also has to be supplied on preflight-less simple GETs for
// media: the browser sends it as a query param ?key=... when used from
// <audio src> / <video src> (where you can't set custom headers).
// Both paths are accepted.
func requireSharedKey(sharedKey string) Middleware {
	keyBytes := []byte(sharedKey)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := r.Header.Get("X-API-Key")
			if got == "" {
				got = r.URL.Query().Get("key")
			}
			if got == "" || subtle.ConstantTimeCompare([]byte(got), keyBytes) != 1 {
				writeError(w, http.StatusUnauthorized, "invalid or missing API key")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// --- CORS ---

// cors reflects allowed origins and exposes the headers the browser
// needs in order to send X-API-Key on cross-origin requests.
func cors(allowed []string) Middleware {
	allowAny := slices.Contains(allowed, "*")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				if allowAny {
					// Echoing the origin (not "*") lets us keep credentials support open.
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
				} else if slices.Contains(allowed, origin) {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
				}
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key")
			w.Header().Set("Access-Control-Max-Age", "600")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// --- recovery ---

// recoverPanic catches panics in handlers, logs them with a stack,
// and returns a 500 so one bad request can't take the server down.
func recoverPanic(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic in handler",
						"panic", rec,
						"method", r.Method,
						"path", r.URL.Path,
						"stack", string(debug.Stack()),
					)
					writeError(w, http.StatusInternalServerError, "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// --- request logging ---

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// logRequests emits one structured log line per request at INFO.
// It redacts query values whose key looks like a secret.
func logRequests(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sr := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(sr, r)

			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"query", redactQuery(r.URL.RawQuery),
				"status", sr.status,
				"bytes", sr.bytes,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote", r.RemoteAddr,
			)
		})
	}
}

func redactQuery(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "&")
	for i, p := range parts {
		k, _, _ := strings.Cut(p, "=")
		if strings.EqualFold(k, "key") || strings.Contains(strings.ToLower(k), "token") {
			parts[i] = k + "=***"
		}
	}
	return strings.Join(parts, "&")
}
