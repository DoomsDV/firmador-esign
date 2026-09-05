package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/DoomsDV/firmador-e/internal/webhook"
)

type panelWebhookRotateRequest struct {
	Environment string `json:"environment"`
}

// handlePanelWebhookRotate genera un nuevo secret, lo cifra y persiste. Se muestra una sola vez.
func (s *Server) handlePanelWebhookRotate(w http.ResponseWriter, r *http.Request) {
	panel := panelFromContext(r.Context())
	var req panelWebhookRotateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "body invalido: "+err.Error())
		return
	}
	env := strings.ToUpper(strings.TrimSpace(req.Environment))
	if env != "TEST" && env != "PROD" {
		writeErr(w, http.StatusUnprocessableEntity, "VALIDATION", "environment debe ser TEST o PROD")
		return
	}

	plain, err := webhook.GenerateSecret()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "SECRET_ERROR", err.Error())
		return
	}
	nonce, ct, err := encHex(s.resolver.MasterKey(), []byte(plain))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ENCRYPT_ERROR", err.Error())
		return
	}

	body := map[string]any{
		"secret_ciphertext": ct,
		"secret_nonce":      nonce,
		"key_version":       1,
	}
	if err := s.resolver.ORDS().StoreWebhookSecret(r.Context(), panel.ClientID, env, body); err != nil {
		writeErr(w, http.StatusBadGateway, "ORDS_ERROR", err.Error())
		return
	}
	writeOK(w, http.StatusOK, map[string]any{
		"environment": env,
		"secret":      plain,
	})
}
