package httpapi

import (
	"compress/gzip"
	"net/http"
	"strings"
)

func Bearer(tokens []string, h http.Handler) http.Handler {
	if len(tokens) == 0 {
		return h
	}
	allowed := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		allowed[t] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), prefix)
		if !ok || !allowed[token] {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			w.Header().Set("WWW-Authenticate", "Bearer")
			return
		}
		h.ServeHTTP(w, r)
	})
}

// CORS wraps h with permissive-allowlist CORS handling: requests whose
// Origin matches an entry in origins (or "*" for any origin) get the
// Access-Control-Allow-Origin header, and OPTIONS preflights are answered
// with 204 before reaching the inner handler. Empty origins disables CORS.
func CORS(origins []string, h http.Handler) http.Handler {
	if len(origins) == 0 {
		return h
	}
	allowed := make(map[string]bool, len(origins))
	for _, o := range origins {
		allowed[o] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			switch {
			case allowed["*"]:
				w.Header().Set("Access-Control-Allow-Origin", "*")
			case allowed[origin]:
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}
		}
		// 预检在鉴权之前应答：浏览器的 OPTIONS 不带 Authorization。
		if r.Method == http.MethodOptions && origin != "" {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "86400")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// Gzip transparently compresses responses for clients that send
// Accept-Encoding: gzip. Mount it inside auth (compression is per-request
// and must come after the credentials check).
func Gzip(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			h.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		h.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, gz: gz}, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) { return w.gz.Write(b) }
