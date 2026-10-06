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

// ServerHeaders 给每个响应附加服务器上下文头：应用身份（X-App-Name /
// X-App-Version）、xyz 库版本（X-XYZ-Version）与 meta.Headers 里的自定义
// 静态头。命令名与耗时由各路由处理器自己写（它们才知道 e.Name 与 Invoke
// 时长）。本中间件应挂在最外层，使 /healthz、/openapi.json 与 /mcp 也带上
// 这些头（xyz-spec §11.6）。
func ServerHeaders(meta ResponseMeta, h http.Handler) http.Handler {
	if !meta.hasAutoContent() && len(meta.Headers) == 0 {
		return h // 无内容可加，零开销直通
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr := w.Header()
		if meta.autoHeaders() {
			if meta.AppName != "" {
				hdr.Set(HeaderAppName, meta.AppName)
			}
			if meta.AppVersion != "" {
				hdr.Set(HeaderAppVersion, meta.AppVersion)
			}
			if meta.SDKVersion != "" {
				hdr.Set(HeaderSDKVersion, meta.SDKVersion)
			}
		}
		for k, v := range meta.Headers {
			hdr.Set(k, v)
		}
		h.ServeHTTP(w, r)
	})
}
