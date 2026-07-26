package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

const (
	ctxKeyPanel ctxKey = iota + 10
)

// PanelClaims son los claims del JWT emitido por ORDS (select-client).
// La clave HMAC son los bytes ASCII de app_parameter.JWT_TOKEN (= ESIGN_JWT_SECRET).
type PanelClaims struct {
	UserID   int    `json:"user_id"`
	ClientID int    `json:"client_id"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// PanelIdentity es lo que queda en el contexto tras validar el JWT del panel.
type PanelIdentity struct {
	UserID   int
	ClientID int
	Role     string
}

// panelJWTMiddleware valida el Bearer JWT del panel (HS256).
// requireOwner=true exige role=owner (subida de secretos).
func (s *Server) panelJWTMiddleware(requireOwner bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(s.jwtSecret) == 0 {
			writeErr(w, http.StatusServiceUnavailable, "JWT_NOT_CONFIGURED", "falta ESIGN_JWT_SECRET en el servidor")
			return
		}
		raw := bearerToken(r)
		if raw == "" {
			writeErr(w, http.StatusUnauthorized, "UNAUTHORIZED", "falta Authorization: Bearer <jwt>")
			return
		}
		claims, err := s.parsePanelJWT(raw)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "INVALID_TOKEN", err.Error())
			return
		}
		if requireOwner && !strings.EqualFold(claims.Role, "owner") {
			writeErr(w, http.StatusForbidden, "FORBIDDEN", "solo el owner puede realizar esta accion")
			return
		}
		id := &PanelIdentity{UserID: claims.UserID, ClientID: claims.ClientID, Role: claims.Role}
		if info := logInfoFromContext(r.Context()); info != nil {
			info.clientID = id.ClientID
		}
		ctx := context.WithValue(r.Context(), ctxKeyPanel, id)
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) parsePanelJWT(tokenStr string) (*PanelClaims, error) {
	claims := &PanelClaims{}
	tok, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("algorithmo inesperado: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	}, jwt.WithIssuer(s.jwtIssuer), jwt.WithAudience(s.jwtAudience))
	if err != nil {
		return nil, fmt.Errorf("token invalido: %w", err)
	}
	if !tok.Valid {
		return nil, fmt.Errorf("token invalido")
	}
	if claims.ClientID <= 0 {
		return nil, fmt.Errorf("claim client_id ausente")
	}
	if claims.Role == "" {
		return nil, fmt.Errorf("claim role ausente")
	}
	return claims, nil
}

func panelFromContext(ctx context.Context) *PanelIdentity {
	id, _ := ctx.Value(ctxKeyPanel).(*PanelIdentity)
	return id
}
