package tenant

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
		Codigo       string   `json:"codigo"`
		Denominacion string   `json:"denominacion"`
		Direccion    string   `json:"direccion"`
		NumCasa      string   `json:"num_casa"`
		Dep          geoJSON  `json:"dep"`
		Dis          geoJSON  `json:"dis"`
		Ciu          geoJSON  `json:"ciu"`
		Telefono     string   `json:"telefono"`
		Email        string   `json:"email"`
		Puntos       []string `json:"puntos"`
	} `json:"establecimientos"`
	SifenEnv *struct {
		NumTimbrado         string `json:"num_timbrado"`
		FechaInicioVigencia string `json:"fecha_inicio_vigencia"`
		IdCSC               string `json:"id_csc"`
		KeyVersion          int    `json:"key_version"`
	} `json:"sifen_env"`
	KudeConfig struct {
		TemplateID      string `json:"template_id"`
		ColorPrimario   string `json:"color_primario"`
		LogoURL         string `json:"logo_url"`
		NotasFooter     string `json:"notas_footer"`
		MostrarFantasia int    `json:"mostrar_fantasia"`
	} `json:"kude_config"`
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
	XMLSHA256       string `json:"xml_sha256,omitempty"`
	XMLSizeBytes    int    `json:"xml_size_bytes,omitempty"`
	XMLMimeType     string `json:"xml_mime_type,omitempty"`
	QRURL           string `json:"qr_url"`
	FromRetry       bool   `json:"from_retry,omitempty"`
	RetryRequested  bool   `json:"retry_requested,omitempty"`
	IdempotencyKey  string `json:"idempotency_key,omitempty"`
}

// IdempotentDocument es el resultado de una emisión previa por Idempotency-Key.
type IdempotentDocument struct {
	CDC             string `json:"cdc"`
	Estado          string `json:"estado"`
	CodRes          string `json:"cod_res"`
	ProtAut         string `json:"prot_aut"`
	MensajeRes      string `json:"mensaje_res"`
	NumeroDocumento string `json:"num_documento"`
	Ambiente        string `json:"ambiente"`
	QRURL           string `json:"qr_url"`
	Phase           string `json:"phase"`
	Found           bool   `json:"found"`
}

// IdempotencyClaim es el resultado del reclamo atómico de una Idempotency-Key.
// ClaimStatus es ACQUIRED, IN_FLIGHT o COMPLETED. COMPLETED incluye el DE previo.
type IdempotencyClaim struct {
	ClaimStatus     string `json:"claim_status"`
	CDC             string `json:"cdc"`
	Estado          string `json:"estado"`
	CodRes          string `json:"cod_res"`
	ProtAut         string `json:"prot_aut"`
	MensajeRes      string `json:"mensaje_res"`
	NumeroDocumento string `json:"num_documento"`
	Ambiente        string `json:"ambiente"`
	QRURL           string `json:"qr_url"`
	Found           bool   `json:"found"`
}

func (c *ORDSClient) RegisterDocument(ctx context.Context, rec DocumentRecord) error {
	if rec.XMLFirmado != "" && rec.XMLSHA256 == "" {
		sum := sha256.Sum256([]byte(rec.XMLFirmado))
		rec.XMLSHA256 = hex.EncodeToString(sum[:])
		rec.XMLSizeBytes = len(rec.XMLFirmado)
		if rec.XMLMimeType == "" {
			rec.XMLMimeType = "application/xml; charset=UTF-8"
		}
	}
	return c.post(ctx, "documents", rec, nil)
}

// ClaimIdempotency reclama una clave antes de la emisión. requestSHA256 vincula
// la clave con el payload normalizado y evita reutilizarla para otro documento.
func (c *ORDSClient) ClaimIdempotency(
	ctx context.Context,
	clientID int,
	env, key, requestSHA256 string,
) (*IdempotencyClaim, error) {
	var out IdempotencyClaim
	err := c.post(ctx, "documents/idempotency/claim", map[string]any{
		"client_id":       clientID,
		"environment":     env,
		"idempotency_key": key,
		"request_sha256":  requestSHA256,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ReleaseIdempotency libera un reclamo que no alcanzó a iniciar el envío a SIFEN.
func (c *ORDSClient) ReleaseIdempotency(ctx context.Context, clientID int, env, key string) error {
	return c.post(ctx, "documents/idempotency/release", map[string]any{
		"client_id":       clientID,
		"environment":     env,
		"idempotency_key": key,
	}, nil)
}

// FindByIdempotencyKey busca un DE ya emitido para (client, env, key). found=false si no hay.
func (c *ORDSClient) FindByIdempotencyKey(ctx context.Context, clientID int, env, key string) (*IdempotentDocument, error) {
	var out IdempotentDocument
	err := c.post(ctx, "documents/by-idempotency", map[string]any{
		"client_id":       clientID,
		"environment":     env,
		"idempotency_key": key,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// PendingRetryDoc es un DE FIRMADO listo para reenvio (XML ya firmado).
type PendingRetryDoc struct {
	CDC             string   `json:"cdc"`
	ClientID        int      `json:"client_id"`
	Environment     string   `json:"environment"`
	Establecimiento string   `json:"establecimiento"`
	PuntoExpedicion string   `json:"punto_expedicion"`
	NumDocumento    string   `json:"num_documento"`
	TipoDE          int      `json:"tipo_de"`
	RetryRequested  int      `json:"retry_requested"`
	RetryCount      int      `json:"retry_count"`
	ReceptorNombre  string   `json:"receptor_nombre"`
	ReceptorDoc     string   `json:"receptor_doc"`
	Moneda          string   `json:"moneda"`
	TotalOperacion  *float64 `json:"total_operacion"`
	QRURL           string   `json:"qr_url"`
	XMLFirmado      string   `json:"xml_firmado"`
	IdempotencyKey  string   `json:"idempotency_key"`
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

// MarkRetryReconciliationRequired detiene la reemisión automática de un DE
// FIRMADO cuando se agotaron los intentos y deja evidencia para conciliación.
func (c *ORDSClient) MarkRetryReconciliationRequired(
	ctx context.Context,
	clientID int,
	cdc, reason string,
) error {
	return c.post(ctx, "documents/retry/reconciliation", map[string]any{
		"client_id": clientID,
		"cdc":       cdc,
		"reason":    reason,
	}, nil)
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

// UploadKude sube el PDF ya renderizado (Gotenberg) al bucket OCI vía el paquete
// PL/SQL (mismo patrón que StoreCertificate/StoreEnvironment: el binario cruza en
// hex). Devuelve la URL pública del objeto. Best-effort: el llamador no debe fallar
// la emisión SIFEN si esto devuelve error.
func (c *ORDSClient) UploadKude(ctx context.Context, clientID int, cdc string, pdf []byte) (string, error) {
	var out struct {
		KudeURL string `json:"kude_url"`
	}
	err := c.post(ctx, "kude", map[string]any{
		"client_id": clientID,
		"cdc":       cdc,
		"pdf_hex":   hex.EncodeToString(pdf),
	}, &out)
	if err != nil {
		return "", err
	}
	return out.KudeURL, nil
}

// GetKude consulta la URL pública (bucket OCI) y el estado de generación del
// KuDE de un documento ya emitido. estado: "pending" | "ready" | "failed".
// Un cdc inexistente para ese client_id devuelve *ORDSError con HTTPStatus 404.
func (c *ORDSClient) GetKude(ctx context.Context, clientID int, cdc string) (kudeURL, estado string, err error) {
	var out struct {
		KudeURL string `json:"kude_url"`
		Estado  string `json:"estado"`
	}
	err = c.post(ctx, "kude/status", map[string]any{"client_id": clientID, "cdc": cdc}, &out)
	if err != nil {
		return "", "", err
	}
	return out.KudeURL, out.Estado, nil
}

// XMLArtifact es el XML firmado canónico + metadatos (solo APROBADO).
type XMLArtifact struct {
	CDC             string `json:"cdc"`
	Estado          string `json:"estado"`
	XMLFirmado      string `json:"xml_firmado"`
	XMLSHA256       string `json:"xml_sha256"`
	XMLSizeBytes    int    `json:"xml_size_bytes"`
	XMLMimeType     string `json:"xml_mime_type"`
	XMLCapturedAt   string `json:"xml_captured_at"`
	XMLAvailability string `json:"xml_availability"`
}

// GetXMLArtifact obtiene el CLOB firmado y metadatos para un CDC APROBADO.
func (c *ORDSClient) GetXMLArtifact(ctx context.Context, clientID int, cdc string) (*XMLArtifact, error) {
	var out XMLArtifact
	err := c.post(ctx, "documents/xml", map[string]any{"client_id": clientID, "cdc": cdc}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// KudeTask es una tarea reclamada de la cola durable de KuDE.
type KudeTask struct {
	TaskID      int64  `json:"task_id"`
	ClientID    int    `json:"client_id"`
	DocumentID  int64  `json:"document_id"`
	CDC         string `json:"cdc"`
	Status      string `json:"status"`
	Attempts    int    `json:"attempts"`
	PayloadJSON string `json:"payload_json"`
}

// KudeRecoveryDocument es un DE APROBADO con XML/QR persistidos al que aún no
// se le creó una tarea durable de KuDE.
type KudeRecoveryDocument struct {
	ClientID    int    `json:"client_id"`
	CDC         string `json:"cdc"`
	Environment string `json:"environment"`
	XMLFirmado  string `json:"xml_firmado"`
	QRURL       string `json:"qr_url"`
}

// ListKudeRecoveryDocuments devuelve documentos aprobados sin tarea KuDE para
// que el worker reconstruya el payload a partir del XML firmado canónico.
func (c *ORDSClient) ListKudeRecoveryDocuments(
	ctx context.Context,
	limit int,
) ([]KudeRecoveryDocument, error) {
	var out []KudeRecoveryDocument
	err := c.post(ctx, "documents/kude-recovery", map[string]any{"limit": limit}, &out)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return []KudeRecoveryDocument{}, nil
	}
	return out, nil
}

// EnqueueKudeTask encola (o reencola) la generación durable del KuDE.
// payloadJSON es el KudeData serializado; vacío conserva el payload previo.
func (c *ORDSClient) EnqueueKudeTask(ctx context.Context, clientID int, cdc, payloadJSON string) error {
	return c.post(ctx, "kude-task/enqueue", map[string]any{
		"client_id":    clientID,
		"cdc":          cdc,
		"payload_json": payloadJSON,
	}, nil)
}

// GetKudeConfig obtiene el branding KuDE del cliente (worker sin API key).
func (c *ORDSClient) GetKudeConfig(ctx context.Context, clientID int) (KudeConfig, error) {
	var out struct {
		TemplateID      string `json:"template_id"`
		ColorPrimario   string `json:"color_primario"`
		LogoURL         string `json:"logo_url"`
		NotasFooter     string `json:"notas_footer"`
		MostrarFantasia int    `json:"mostrar_fantasia"`
	}
	err := c.post(ctx, "kude-config", map[string]any{"client_id": clientID}, &out)
	if err != nil {
		return KudeConfig{}, err
	}
	return KudeConfig{
		TemplateID:      out.TemplateID,
		ColorPrimario:   out.ColorPrimario,
		LogoURL:         out.LogoURL,
		NotasFooter:     out.NotasFooter,
		MostrarFantasia: out.MostrarFantasia != 0,
	}, nil
}

// ClaimKudeTasks reclama hasta limit tareas con lease.
func (c *ORDSClient) ClaimKudeTasks(ctx context.Context, leaseOwner string, leaseSeconds, limit int) ([]KudeTask, error) {
	var out []KudeTask
	err := c.post(ctx, "kude-task/claim", map[string]any{
		"lease_owner":   leaseOwner,
		"lease_seconds": leaseSeconds,
		"limit":         limit,
	}, &out)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return []KudeTask{}, nil
	}
	return out, nil
}

// CompleteKudeTask marca READY (success) o reencola/FAILED (error).
func (c *ORDSClient) CompleteKudeTask(ctx context.Context, taskID int64, success bool, errMsg string) error {
	return c.post(ctx, "kude-task/complete", map[string]any{
		"task_id": taskID,
		"success": success,
		"error":   errMsg,
	}, nil)
}

// WebhookSecretResponse mapea pr_get_secret_json.
type WebhookSecretResponse struct {
	SecretCiphertext string `json:"secret_ciphertext"`
	SecretNonce      string `json:"secret_nonce"`
	KeyVersion       int    `json:"key_version"`
	URL              string `json:"url"`
	IsActive         int    `json:"is_active"`
}

// WebhookDelivery es una entrega reclamada de la cola webhook.
type WebhookDelivery struct {
	DeliveryID     int64  `json:"delivery_id"`
	ClientID       int    `json:"client_id"`
	DocumentID     int64  `json:"document_id"`
	CDC            string `json:"cdc"`
	Environment    string `json:"environment"`
	EventType      string `json:"event_type"`
	EventID        string `json:"event_id"`
	DeliveryUUID   string `json:"delivery_uuid"`
	TargetURL      string `json:"target_url"`
	Attempts       int    `json:"attempts"`
	IdempotencyKey string `json:"idempotency_key"`
	ProtAut        string `json:"prot_aut"`
	KudeURL        string `json:"kude_url"`
	XMLSHA256      string `json:"xml_sha256"`
	XMLSizeBytes   int    `json:"xml_size_bytes"`
	XMLMimeType    string `json:"xml_mime_type"`
}

// WebhookRecoveryDoc documento sin entrega DELIVERED.
type WebhookRecoveryDoc struct {
	ClientID int    `json:"client_id"`
	CDC      string `json:"cdc"`
}

func (c *ORDSClient) StoreWebhookSecret(ctx context.Context, clientID int, env string, body map[string]any) error {
	body["client_id"] = clientID
	body["environment"] = env
	return c.post(ctx, "webhook/store-secret", body, nil)
}

func (c *ORDSClient) GetWebhookSecret(ctx context.Context, clientID int, env string) (*WebhookSecretResponse, error) {
	var out WebhookSecretResponse
	err := c.post(ctx, "webhook/secret", map[string]any{
		"client_id":   clientID,
		"environment": env,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *ORDSClient) EnqueueWebhookDelivery(ctx context.Context, clientID int, cdc string) error {
	return c.post(ctx, "webhook-delivery/enqueue", map[string]any{
		"client_id": clientID,
		"cdc":       cdc,
	}, nil)
}

func (c *ORDSClient) ClaimWebhookDeliveries(ctx context.Context, leaseOwner string, leaseSeconds, limit int) ([]WebhookDelivery, error) {
	var out []WebhookDelivery
	err := c.post(ctx, "webhook-delivery/claim", map[string]any{
		"lease_owner":   leaseOwner,
		"lease_seconds": leaseSeconds,
		"limit":         limit,
	}, &out)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return []WebhookDelivery{}, nil
	}
	return out, nil
}

func (c *ORDSClient) CompleteWebhookDelivery(
	ctx context.Context,
	deliveryID int64,
	success bool,
	httpStatus int,
	errMsg string,
	retryable bool,
) error {
	return c.post(ctx, "webhook-delivery/complete", map[string]any{
		"delivery_id": deliveryID,
		"success":     success,
		"http_status": httpStatus,
		"error":       errMsg,
		"retryable":   retryable,
	}, nil)
}

func (c *ORDSClient) ListWebhookRecovery(ctx context.Context, limit int) ([]WebhookRecoveryDoc, error) {
	var out []WebhookRecoveryDoc
	err := c.post(ctx, "webhook-delivery/recovery", map[string]any{"limit": limit}, &out)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return []WebhookRecoveryDoc{}, nil
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
