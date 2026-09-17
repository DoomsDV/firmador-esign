package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/DoomsDV/firmador-e/internal/sifen"
	"github.com/DoomsDV/firmador-e/internal/tenant"
)

// handleReconcileDocument consulta el estado autoritativo de SIFEN para un
// documento del cliente autenticado y aplica solo transiciones terminales.
func (s *Server) handleReconcileDocument(w http.ResponseWriter, r *http.Request) {
	cfg := tenantFromContext(r.Context())
	result, err := s.reconcileDocument(r.Context(), cfg.ClientID, r.PathValue("cdc"))
	if err != nil {
		s.writeReconcileErr(w, err)
		return
	}
	writeOK(w, http.StatusOK, result)
}

// handlePanelReconcileDocument ofrece la misma operación sin exponer la API
// key al navegador. El JWT limita el CDC al client_id del usuario del panel.
func (s *Server) handlePanelReconcileDocument(w http.ResponseWriter, r *http.Request) {
	identity := panelFromContext(r.Context())
	if identity == nil {
		writeErr(w, http.StatusUnauthorized, "PANEL_AUTH_REQUIRED", "sesión de panel requerida")
		return
	}
	result, err := s.reconcileDocument(r.Context(), identity.ClientID, r.PathValue("cdc"))
	if err != nil {
		s.writeReconcileErr(w, err)
		return
	}
	writeOK(w, http.StatusOK, result)
}

func (s *Server) reconcileDocument(ctx context.Context, clientID int, cdc string) (reconcileResponse, error) {
	if !validCDC(cdc) {
		return reconcileResponse{}, &reconcileError{status: http.StatusUnprocessableEntity, code: "INVALID_CDC", message: "CDC inválido"}
	}
	local, err := s.resolver.ORDS().GetReconcileContext(ctx, clientID, cdc)
	if err != nil {
		return reconcileResponse{}, err
	}
	env, err := sifen.ParseEnvironment(local.Environment)
	if err != nil {
		return reconcileResponse{}, fmt.Errorf("ambiente local inválido: %w", err)
	}
	cert, err := s.resolver.GetCertificateByClientID(ctx, clientID)
	if err != nil {
		return reconcileResponse{}, fmt.Errorf("certificado: %w", err)
	}
	client, err := sifen.NewClientFromP12Bytes(env, cert.P12, cert.Password, sifen.EndpointsFor(env).WsSync)
	if err != nil {
		return reconcileResponse{}, fmt.Errorf("cliente SOAP: %w", err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	_, body, err := client.ConsultarDE(queryCtx, int64(eventID()), sifen.EndpointsFor(env).WsConsulta, cdc)
	if err != nil {
		return reconcileResponse{}, fmt.Errorf("consulta SIFEN: %w", err)
	}
	remote := sifen.ParseConsultaResult(body)
	response := reconcileResponse{
		CDC: cdc, Estado: local.Estado, Ambiente: local.Environment,
		Found: remote.Found, Cancelado: remote.Cancelado, CodRes: remote.CodRes,
		ProtAut: remote.ProtAut, Mensaje: remote.MsgRes,
		RequiresReconciliation: local.RecoveryRequired,
	}
	if remote.CodRes == "0420" {
		return response, nil
	}
	if !remote.Found {
		return reconcileResponse{}, &reconcileError{status: http.StatusBadGateway, code: "SIFEN_QUERY_INVALID", message: "SIFEN devolvió una consulta sin estado autoritativo"}
	}
	remoteState := "APROBADO"
	if remote.Cancelado {
		remoteState = "CANCELADO"
	}
	updated, err := s.resolver.ORDS().ReconcileDocument(ctx, clientID, cdc, local.Environment, remoteState, remote.CodRes, remote.ProtAut, remote.MsgRes)
	if err != nil {
		return reconcileResponse{}, fmt.Errorf("persistir conciliación: %w", err)
	}
	response.Estado = updated.Estado
	response.RequiresReconciliation = updated.RecoveryRequired
	return response, nil
}

type reconcileError struct {
	status  int
	code    string
	message string
}

func (e *reconcileError) Error() string { return e.message }

func (s *Server) writeReconcileErr(w http.ResponseWriter, err error) {
	var custom *reconcileError
	if errors.As(err, &custom) {
		writeErr(w, custom.status, custom.code, custom.message)
		return
	}
	writeDocumentContextErr(w, err)
}

func writeDocumentContextErr(w http.ResponseWriter, err error) {
	var ordsErr *tenant.ORDSError
	if errors.As(err, &ordsErr) && ordsErr.HTTPStatus == http.StatusNotFound {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "documento inexistente")
		return
	}
	if strings.Contains(err.Error(), "documento inexistente") {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "documento inexistente")
		return
	}
	writeErr(w, http.StatusBadGateway, "ORDS_ERROR", err.Error())
}
