package httpapi

import (
	"net/http"
	"strings"
)

const corsMaxAge = "86400"

var defaultCORSOrigins = []string{
	"https://staging.etick.uno",
	"https://etick.uno",
	"http://localhost:5173",
}

// parseCORSOrigins construye la allowlist desde ESIGN_CORS_ORIGINS (comma-separated).
// Si raw está vacío, usa los orígenes por defecto del panel.
func parseCORSOrigins(raw string) map[string]struct{} {
	raw = strings.TrimSpace(raw)
	var parts []string
	if raw == "" {
		parts = defaultCORSOrigins
	} else {
		for _, p := range strings.Split(raw, ",") {
			if o := strings.TrimSpace(p); o != "" {
				parts = append(parts, o)
			}
		}
	}
	allowed := make(map[string]struct{}, len(parts))
	for _, o := range parts {
		allowed[o] = struct{}{}
	}
	return allowed
}

// corsMiddleware responde preflight OPTIONS y añade cabeceras CORS para orígenes
// permitidos. Debe envolver el mux completo (más externo que withLogging/auth).
func corsMiddleware(allowed map[string]struct{}) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin != "" {
				if _, ok := allowed[origin]; ok {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")
					w.Header().Set("Access-Control-Max-Age", corsMaxAge)
				}
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
