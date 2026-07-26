package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/DoomsDV/firmador-e/internal/sifen"
	"github.com/DoomsDV/firmador-e/internal/tenant"
)

type ctxKey int

const (
	ctxKeyTenant ctxKey = iota
	ctxKeyAPIKey
)

// prefixEnv deriva el ambiente del prefijo de la API key. Es la única fuente de
// verdad del ambiente en toda la cadena (regla del plan: la key manda).
func prefixEnv(apiKey string) (sifen.Environment, bool) {
	switch {
	case strings.HasPrefix(apiKey, "sk_test_"):
		return sifen.EnvTest, true
	case strings.HasPrefix(apiKey, "sk_prod_"):
		return sifen.EnvProd, true
	default:
		return "", false
	}
}

// authMiddleware valida la API key (Bearer sk_test_/sk_prod_), resuelve el tenant
// y lo inyecta en el contexto. Rechaza claves ausentes, con prefijo inválido, o de
// tenants no activos.
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		apiKey := bearerToken(r)
		if apiKey == "" {
			writeErr(w, http.StatusUnauthorized, "UNAUTHORIZED", "falta Authorization: Bearer sk_test_/sk_prod_...")
			return
		}
		env, ok := prefixEnv(apiKey)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "INVALID_KEY_PREFIX", "la API key debe empezar con sk_test_ o sk_prod_")
			return
		}
		cfg, err := s.resolver.Resolve(r.Context(), apiKey, env)
		if err != nil {
			s.handleResolveError(w, err)
			return
		}
		if !strings.EqualFold(cfg.Status, "ACTIVE") {
			writeErr(w, http.StatusForbidden, "CLIENT_INACTIVE", "el cliente no está activo (status="+cfg.Status+")")
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyTenant, cfg)
		ctx = context.WithValue(ctx, ctxKeyAPIKey, apiKey)
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) handleResolveError(w http.ResponseWriter, err error) {
	var ordsErr *tenant.ORDSError
	if errors.As(err, &ordsErr) {
		switch ordsErr.Code {
		case "UNAUTHORIZED":
			writeErr(w, http.StatusUnauthorized, "UNAUTHORIZED", ordsErr.Message)
		case "KEY_REVOKED":
			writeErr(w, http.StatusForbidden, "KEY_REVOKED", ordsErr.Message)
		default:
			writeErr(w, http.StatusUnauthorized, "INVALID_KEY", ordsErr.Message)
		}
		return
	}
	writeErr(w, http.StatusUnauthorized, "INVALID_KEY", err.Error())
}

func bearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return auth
}

func tenantFromContext(ctx context.Context) *tenant.Config {
	cfg, _ := ctx.Value(ctxKeyTenant).(*tenant.Config)
	return cfg
}

func apiKeyFromContext(ctx context.Context) string {
	k, _ := ctx.Value(ctxKeyAPIKey).(string)
	return k
}
