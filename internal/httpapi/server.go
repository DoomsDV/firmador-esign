package httpapi

import (
	"net/http"
	"time"

	"github.com/DoomsDV/firmador-e/internal/tenant"
)

// Server expone el motor de emisión SIFEN como API HTTP multi-tenant.
type Server struct {
	resolver *tenant.Resolver
	tz       *time.Location
}

// New crea el servidor con el resolver de tenants ya configurado.
func New(resolver *tenant.Resolver) *Server {
	tz, err := time.LoadLocation("America/Asuncion")
	if err != nil {
		tz = time.FixedZone("PYT", -3*60*60)
	}
	return &Server{resolver: resolver, tz: tz}
}

// Handler arma el router. /v1/health es público; el resto exige API key.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.withLogging(s.handleHealth))
	mux.HandleFunc("POST /v1/documents", s.withLogging(s.authMiddleware(s.handleCreateDocument)))
	mux.HandleFunc("POST /v1/documents/{cdc}/cancel", s.withLogging(s.authMiddleware(s.handleCancelDocument)))
	mux.HandleFunc("POST /v1/events/inutilizacion", s.withLogging(s.authMiddleware(s.handleInutilizacion)))
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeOK(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// fechaFirma devuelve la hora de Asunción con margen atrás (evita error 1004:
// firma adelantada respecto del reloj de SIFEN).
func (s *Server) fechaFirma() time.Time {
	return time.Now().In(s.tz).Add(-5 * time.Minute)
}
