package httpapi

import (
	"encoding/json"
	"net/http"
)

// envelope estándar de respuesta de la API Go (mismo contrato que ORDS).
type okEnvelope struct {
	Success bool `json:"success"`
	Data    any  `json:"data"`
}

type errEnvelope struct {
	Success bool      `json:"success"`
	Data    any       `json:"data"`
	Error   *apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeOK(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, okEnvelope{Success: true, Data: data})
}

func writeErr(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errEnvelope{Success: false, Error: &apiError{Code: code, Message: message}})
}
