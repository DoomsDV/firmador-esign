package httpapi

import (
	"net/http"
	"time"

	"github.com/DoomsDV/firmador-e/internal/gotenberg"
	"github.com/DoomsDV/firmador-e/internal/tenant"
)

// ServerOptions configura JWT del panel y otros extras del HTTP API.
type ServerOptions struct {
	JWTSecret   []byte // bytes ASCII de app_parameter.JWT_TOKEN
	JWTIssuer   string // default esign-api
	JWTAudience string // default esign-app

	// GotenbergURL es la base del servicio Gotenberg usado para renderizar el
	// KuDE (PDF). Vacío deshabilita la generación (queda logueada como warning).
	GotenbergURL string
	// KudeTimeout acota el render+subida asíncrona del KuDE. <=0 usa el default.
	KudeTimeout time.Duration
}

// Server expone el motor de emisión SIFEN como API HTTP multi-tenant.
type Server struct {
	resolver    *tenant.Resolver
	tz          *time.Location
	jwtSecret   []byte
	jwtIssuer   string
	jwtAudience string

	gotenberg   *gotenberg.Client
	kudeTimeout time.Duration
}

// New crea el servidor con el resolver de tenants ya configurado.
func New(resolver *tenant.Resolver, opts ServerOptions) *Server {
	tz, err := time.LoadLocation("America/Asuncion")
	if err != nil {
		tz = time.FixedZone("PYT", -3*60*60)
	}
	issuer := opts.JWTIssuer
	if issuer == "" {
		issuer = "esign-api"
	}
	aud := opts.JWTAudience
	if aud == "" {
		aud = "esign-app"
	}
	kudeTimeout := opts.KudeTimeout
	if kudeTimeout <= 0 {
		kudeTimeout = gotenberg.DefaultTimeout
	}
	return &Server{
		resolver:    resolver,
		tz:          tz,
		jwtSecret:   opts.JWTSecret,
		jwtIssuer:   issuer,
		jwtAudience: aud,
		gotenberg:   gotenberg.New(opts.GotenbergURL, kudeTimeout),
		kudeTimeout: kudeTimeout,
	}
}

// Handler arma el router. /v1/health es público; emisión exige API key;
// /v1/panel/* exige JWT del panel (owner para secretos).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.withLogging(s.handleHealth))
	mux.HandleFunc("POST /v1/documents", s.withLogging(s.authMiddleware(s.handleCreateDocument)))
	mux.HandleFunc("POST /v1/documents/{cdc}/cancel", s.withLogging(s.authMiddleware(s.handleCancelDocument)))
	mux.HandleFunc("POST /v1/events/inutilizacion", s.withLogging(s.authMiddleware(s.handleInutilizacion)))
	mux.HandleFunc("POST /v1/panel/certificate", s.withLogging(s.panelJWTMiddleware(true, s.handlePanelCertificate)))
	mux.HandleFunc("PUT /v1/panel/environments", s.withLogging(s.panelJWTMiddleware(true, s.handlePanelEnvironments)))
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
