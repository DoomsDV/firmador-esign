// Package webhook implementa el worker que reclama entregas durables y POSTea
// eventos invoice.ready con firma HMAC al endpoint configurado por el tenant.
package webhook

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/DoomsDV/firmador-e/internal/tenant"
)

// Worker reclama document_webhook_delivery y entrega eventos firmados.
type Worker struct {
	resolver *tenant.Resolver
	cfg      Config
	owner    string
	client   *http.Client
}

// New crea el worker. Si Enabled=false, Start es no-op.
func New(resolver *tenant.Resolver, cfg Config) *Worker {
	if cfg.Interval <= 0 {
		cfg.Interval = 15 * time.Second
	}
	if cfg.Batch <= 0 {
		cfg.Batch = 10
	}
	if cfg.LeaseSeconds <= 0 {
		cfg.LeaseSeconds = 120
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	cfg.PublicAPIURL = strings.TrimRight(strings.TrimSpace(cfg.PublicAPIURL), "/")
	owner, _ := os.Hostname()
	if owner == "" {
		owner = "webhook-worker"
	}
	return &Worker{
		resolver: resolver,
		cfg:      cfg,
		client:   &http.Client{Timeout: cfg.Timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}
}

// Start lanza el loop hasta que ctx se cancele.
func (w *Worker) Start(ctx context.Context) {
	if !w.cfg.Enabled {
		log.Printf("ℹ️  webhook delivery worker desactivado (ESIGN_WEBHOOK_QUEUE_ENABLED=false)")
		return
	}
	log.Printf("🔔 webhook delivery worker activo (intervalo=%s, batch=%d, lease=%ds)",
		w.cfg.Interval, w.cfg.Batch, w.cfg.LeaseSeconds)
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()

	w.cycle(ctx)
	for {
		select {
		case <-ctx.Done():
			log.Printf("🔔 webhook delivery worker detenido")
			return
		case <-ticker.C:
			w.cycle(ctx)
		}
	}
}

func (w *Worker) cycle(ctx context.Context) {
	w.reconcileMissing(ctx)

	deliveries, err := w.resolver.ORDS().ClaimWebhookDeliveries(ctx, w.owner, w.cfg.LeaseSeconds, w.cfg.Batch)
	if err != nil {
		log.Printf("⚠️  webhook queue: claim: %v", err)
		return
	}
	for i := range deliveries {
		if ctx.Err() != nil {
			return
		}
		if err := w.processOne(ctx, &deliveries[i]); err != nil {
			log.Printf("⚠️  webhook delivery id=%d cdc=%s: %v", deliveries[i].DeliveryID, deliveries[i].CDC, err)
		}
	}
}

func (w *Worker) reconcileMissing(ctx context.Context) {
	docs, err := w.resolver.ORDS().ListWebhookRecovery(ctx, w.cfg.Batch)
	if err != nil {
		log.Printf("⚠️  webhook queue: recuperar pendientes: %v", err)
		return
	}
	for i := range docs {
		if ctx.Err() != nil {
			return
		}
		if err := w.resolver.ORDS().EnqueueWebhookDelivery(ctx, docs[i].ClientID, docs[i].CDC); err != nil {
			log.Printf("⚠️  webhook recovery cdc=%s: %v", docs[i].CDC, err)
		}
	}
}

func (w *Worker) processOne(ctx context.Context, d *tenant.WebhookDelivery) error {
	if strings.TrimSpace(d.TargetURL) == "" {
		msg := "target_url vacía"
		_ = w.resolver.ORDS().CompleteWebhookDelivery(ctx, d.DeliveryID, false, 0, msg, false)
		return fmt.Errorf("%s", msg)
	}
	if err := ValidateWebhookURL(d.TargetURL); err != nil {
		msg := "url inválida: " + err.Error()
		_ = w.resolver.ORDS().CompleteWebhookDelivery(ctx, d.DeliveryID, false, 0, msg, false)
		return fmt.Errorf("%s", msg)
	}

	secretResp, err := w.resolver.ORDS().GetWebhookSecret(ctx, d.ClientID, d.Environment)
	if err != nil {
		msg := "secret: " + err.Error()
		_ = w.resolver.ORDS().CompleteWebhookDelivery(ctx, d.DeliveryID, false, 0, msg, true)
		return fmt.Errorf("%s", msg)
	}
	secret, err := tenant.DecryptHex(w.resolver.MasterKey(), secretResp.SecretNonce, secretResp.SecretCiphertext)
	if err != nil {
		msg := "descifrar secret: " + err.Error()
		_ = w.resolver.ORDS().CompleteWebhookDelivery(ctx, d.DeliveryID, false, 0, msg, true)
		return fmt.Errorf("%s", msg)
	}

	xmlURL := w.xmlPublicURL(d.CDC)
	payload := EventPayload{
		EventID:        d.EventID,
		Event:          d.EventType,
		OccurredAt:     time.Now().UTC().Format(time.RFC3339),
		Environment:    strings.ToLower(d.Environment),
		CDC:            d.CDC,
		IdempotencyKey: d.IdempotencyKey,
		Estado:         "APROBADO",
		ProtAut:        d.ProtAut,
		KudeURL:        d.KudeURL,
		XMLSHA256:      d.XMLSHA256,
		XMLSize:        d.XMLSizeBytes,
		XMLMime:        d.XMLMimeType,
		XMLURL:         xmlURL,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		msg := "serializar payload: " + err.Error()
		_ = w.resolver.ORDS().CompleteWebhookDelivery(ctx, d.DeliveryID, false, 0, msg, false)
		return fmt.Errorf("%s", msg)
	}

	ts, sig := SignHeaders(secret, body, time.Now())
	runCtx, cancel := context.WithTimeout(ctx, w.cfg.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(runCtx, http.MethodPost, d.TargetURL, bytes.NewReader(body))
	if err != nil {
		msg := "crear request: " + err.Error()
		_ = w.resolver.ORDS().CompleteWebhookDelivery(ctx, d.DeliveryID, false, 0, msg, true)
		return fmt.Errorf("%s", msg)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Esign-Timestamp", ts)
	req.Header.Set("X-Esign-Signature", sig)
	req.Header.Set("X-Esign-Delivery-Id", d.DeliveryUUID)
	req.Header.Set("User-Agent", "esign-webhook/1.0")

	resp, err := w.client.Do(req)
	if err != nil {
		msg := "POST: " + err.Error()
		_ = w.resolver.ORDS().CompleteWebhookDelivery(ctx, d.DeliveryID, false, 0, msg, true)
		return fmt.Errorf("%s", msg)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	status := resp.StatusCode
	success := status >= 200 && status < 300
	retryable := !success && (status == 409 || status == 429 || status >= 500)
	if !success && status >= 400 && status < 500 && status != 409 {
		retryable = false
	}

	var errMsg string
	if !success {
		errMsg = fmt.Sprintf("http %d", status)
	}
	if err := w.resolver.ORDS().CompleteWebhookDelivery(ctx, d.DeliveryID, success, status, errMsg, retryable); err != nil {
		return fmt.Errorf("complete: %w", err)
	}
	if success {
		log.Printf("✅ webhook delivery cdc=%s → %d", d.CDC, status)
		return nil
	}
	return fmt.Errorf("entrega fallida: http %d (retryable=%v)", status, retryable)
}

func (w *Worker) xmlPublicURL(cdc string) string {
	if w.cfg.PublicAPIURL == "" {
		return ""
	}
	return w.cfg.PublicAPIURL + "/v1/documents/" + cdc + "/xml"
}

// EnqueueAfterKude encola la entrega webhook tras KuDE listo (best-effort).
func EnqueueAfterKude(ctx context.Context, ords *tenant.ORDSClient, clientID int, cdc string) {
	if err := ords.EnqueueWebhookDelivery(ctx, clientID, cdc); err != nil {
		log.Printf("[warn] webhook enqueue %s: %v", cdc, err)
	}
}

// GenerateSecret genera un secret webhook aleatorio (whsec_ + 32 bytes hex).
func GenerateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "whsec_" + hex.EncodeToString(b), nil
}
