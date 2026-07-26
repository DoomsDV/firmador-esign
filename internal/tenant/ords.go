package tenant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ORDSClient habla con el módulo interno de ORDS (/internal/v1/) autenticado con
// X-Service-Token. Cada método envuelve un handler PL/SQL que devuelve el envelope
// estándar {success, data, error}.
type ORDSClient struct {
	baseURL      string // ej. https://<host>/ords/esign (sin barra final)
	serviceToken string
	http         *http.Client
}

// NewORDSClient crea el cliente. baseURL es la raíz del esquema ORDS (sin el
// prefijo del módulo); las rutas internas cuelgan de {baseURL}/internal/v1/...
func NewORDSClient(baseURL, serviceToken string) *ORDSClient {
	return &ORDSClient{
		baseURL:      strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		serviceToken: serviceToken,
		http:         &http.Client{Timeout: 30 * time.Second},
	}
}

// envelope es la respuesta estándar de los handlers ORDS.
type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Meta    json.RawMessage `json:"meta"`
	Error   *envelopeError  `json:"error"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *envelopeError) String() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// post envía body como JSON a {baseURL}/internal/v1/{path}, valida el envelope y
// deserializa data en out (si out != nil).
func (c *ORDSClient) post(ctx context.Context, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("serializar request %s: %w", path, err)
	}
	url := c.baseURL + "/internal/v1/" + strings.TrimLeft(path, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("crear request %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Service-Token", c.serviceToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("leer respuesta %s: %w", path, err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("ORDS %s: 401 (X-Service-Token inválido)", path)
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("ORDS %s: respuesta no-JSON (http %d): %s", path, resp.StatusCode, truncate(string(raw), 300))
	}
	if !env.Success {
		if env.Error != nil {
			return &ORDSError{Path: path, HTTPStatus: resp.StatusCode, Code: env.Error.Code, Message: env.Error.Message}
		}
		return fmt.Errorf("ORDS %s: success=false (http %d)", path, resp.StatusCode)
	}
	if out != nil && len(env.Data) > 0 && string(env.Data) != "null" {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("ORDS %s: deserializar data: %w", path, err)
		}
	}
	return nil
}

// ORDSError representa un fallo lógico devuelto por un handler ORDS.
type ORDSError struct {
	Path       string
	HTTPStatus int
	Code       string
	Message    string
}

func (e *ORDSError) Error() string {
	return fmt.Sprintf("ORDS %s: %s (%s)", e.Path, e.Message, e.Code)
}

// --- DTOs de las respuestas internas ---------------------------------------

// contextResponse mapea el JSON de pr_get_internal_context.
type contextResponse struct {
	ClientID      int    `json:"client_id"`
	BusinessName  string `json:"business_name"`
	RUC           string `json:"ruc"`
	DV            int    `json:"dv"`
	Status        string `json:"status"`
	Environment   string `json:"environment"`
	CertAvailable bool   `json:"cert_available"`
	Emisor        struct {
		TipoContribuyente int    `json:"tipo_contribuyente"`
		TipoRegimen       int    `json:"tipo_regimen"`
		NombreFantasia    string `json:"nombre_fantasia"`
		Actividades       []struct {
			Cod  string `json:"cod"`
			Desc string `json:"desc"`
		} `json:"actividades"`
	} `json:"emisor"`
	Establecimientos []struct {
		Codigo       string  `json:"codigo"`
		Denominacion string  `json:"denominacion"`
		Direccion    string  `json:"direccion"`
		NumCasa      string  `json:"num_casa"`
		Dep          geoJSON `json:"dep"`
		Dis          geoJSON `json:"dis"`
		Ciu          geoJSON `json:"ciu"`
		Telefono     string  `json:"telefono"`
		Email        string  `json:"email"`
		Puntos       []string `json:"puntos"`
	} `json:"establecimientos"`
	SifenEnv *struct {
		NumTimbrado         string `json:"num_timbrado"`
		FechaInicioVigencia string `json:"fecha_inicio_vigencia"`
		IdCSC               string `json:"id_csc"`
		KeyVersion          int    `json:"key_version"`
	} `json:"sifen_env"`
}

type geoJSON struct {
	Cod  int    `json:"cod"`
	Desc string `json:"desc"`
}

// certificateResponse mapea pr_get_certificate_json (blobs cifrados en hex).
type certificateResponse struct {
	P12Ciphertext string `json:"p12_ciphertext"`
	P12Nonce      string `json:"p12_nonce"`
	PwdCiphertext string `json:"pwd_ciphertext"`
	PwdNonce      string `json:"pwd_nonce"`
	KeyVersion    int    `json:"key_version"`
}

// cscResponse mapea pr_get_csc_json (CSC cifrado en hex).
type cscResponse struct {
	IdCSC         string `json:"id_csc"`
	CSCCiphertext string `json:"csc_ciphertext"`
	CSCNonce      string `json:"csc_nonce"`
	KeyVersion    int    `json:"key_version"`
}

// --- Métodos internos -------------------------------------------------------

func (c *ORDSClient) resolveContext(ctx context.Context, apiKey string) (*contextResponse, error) {
	var out contextResponse
	err := c.post(ctx, "context", map[string]string{"api_key": apiKey}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *ORDSClient) getCertificate(ctx context.Context, clientID int) (*certificateResponse, error) {
	var out certificateResponse
	err := c.post(ctx, "certificate", map[string]any{"client_id": clientID}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *ORDSClient) getCSC(ctx context.Context, clientID int, env string) (*cscResponse, error) {
	var out cscResponse
	err := c.post(ctx, "csc", map[string]any{"client_id": clientID, "environment": env}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// NextNumber pide el correlativo (sin huecos) para el trío (est, punto, tipoDE)
// del ambiente. Bloqueo/commit ocurren en el handler PL/SQL.
func (c *ORDSClient) NextNumber(ctx context.Context, clientID int, env, est, punto string, tipoDE int) (int, error) {
	var out struct {
		Number int `json:"number"`
	}
	err := c.post(ctx, "next-number", map[string]any{
		"client_id":        clientID,
		"environment":      env,
		"establecimiento":  est,
		"punto_expedicion": punto,
		"tipo_de":          tipoDE,
	}, &out)
	if err != nil {
		return 0, err
	}
	return out.Number, nil
}

// DocumentRecord es lo que se persiste tras firmar/enviar un DE.
type DocumentRecord struct {
	ClientID        int    `json:"client_id"`
	Environment     string `json:"environment"`
	TipoDE          int    `json:"tipo_de"`
	CDC             string `json:"cdc"`
	NumeroDoc       string `json:"num_documento"`
	Establecimiento string `json:"establecimiento"`
	PuntoExpedicion string `json:"punto_expedicion"`
	ReceptorNombre  string `json:"receptor_nombre"`
	ReceptorDoc     string `json:"receptor_doc"`
	Moneda          string `json:"moneda"`
	TotalOperacion  string `json:"total_operacion"`
	Estado          string `json:"estado"`
	CodRes          string `json:"cod_res"`
	ProtAut         string `json:"prot_aut"`
	MensajeRes      string `json:"mensaje_res"`
	XMLFirmado      string `json:"xml_firmado"`
	QRURL           string `json:"qr_url"`
	FromRetry       bool   `json:"from_retry,omitempty"`
}

func (c *ORDSClient) RegisterDocument(ctx context.Context, rec DocumentRecord) error {
	return c.post(ctx, "documents", rec, nil)
}

// PendingRetryDoc es un DE FIRMADO listo para reenvio (XML ya firmado).
type PendingRetryDoc struct {
	CDC             string  `json:"cdc"`
	ClientID        int     `json:"client_id"`
	Environment     string  `json:"environment"`
	Establecimiento string  `json:"establecimiento"`
	PuntoExpedicion string  `json:"punto_expedicion"`
	NumDocumento    string  `json:"num_documento"`
	TipoDE          int     `json:"tipo_de"`
	RetryRequested  int     `json:"retry_requested"`
	RetryCount      int     `json:"retry_count"`
	ReceptorNombre  string  `json:"receptor_nombre"`
	ReceptorDoc     string  `json:"receptor_doc"`
	Moneda          string  `json:"moneda"`
	TotalOperacion  *float64 `json:"total_operacion"`
	QRURL           string  `json:"qr_url"`
	XMLFirmado      string  `json:"xml_firmado"`
}

// ListPendingRetry pide documentos FIRMADO pendientes. mode: "flagged" | "all".
func (c *ORDSClient) ListPendingRetry(ctx context.Context, mode string, limit int) ([]PendingRetryDoc, error) {
	var out []PendingRetryDoc
	err := c.post(ctx, "documents/pending-retry", map[string]any{
		"mode":  mode,
		"limit": limit,
	}, &out)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return []PendingRetryDoc{}, nil
	}
	return out, nil
}

// StoreCertificate persiste un certificado YA cifrado (hex) vía ORDS interno.
func (c *ORDSClient) StoreCertificate(ctx context.Context, body map[string]any) error {
	return c.post(ctx, "certificate/store", body, nil)
}

// StoreEnvironment persiste timbrado + CSC YA cifrado (hex) vía ORDS interno.
func (c *ORDSClient) StoreEnvironment(ctx context.Context, body map[string]any) error {
	return c.post(ctx, "environments/store", body, nil)
}

// EventRecord es lo que se persiste tras un evento (cancelación/inutilización).
type EventRecord struct {
	ClientID   int    `json:"client_id"`
	CDC        string `json:"cdc"`
	TipoEvento string `json:"tipo_evento"` // CANCELACION | INUTILIZACION
	Estado     string `json:"estado"`
	CodRes     string `json:"cod_res"`
	ProtAut    string `json:"prot_aut"`
	Motivo     string `json:"motivo"`
}

func (c *ORDSClient) RegisterEvent(ctx context.Context, rec EventRecord) error {
	return c.post(ctx, "events", rec, nil)
}

// LogEntry registra una llamada a la API.
type LogEntry struct {
	ClientID    int    `json:"client_id,omitempty"`
	Environment string `json:"environment,omitempty"`
	Endpoint    string `json:"endpoint"`
	HTTPStatus  int    `json:"http_status"`
	LatencyMS   int64  `json:"latency_ms"`
}

func (c *ORDSClient) Log(ctx context.Context, entry LogEntry) error {
	return c.post(ctx, "logs", entry, nil)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
