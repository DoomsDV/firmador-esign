// Package retry implementa el worker que reenvía a SIFEN los documentos en estado
// FIRMADO (fallo transitorio de red). Regla 0141: reenvía el xml_firmado EXACTO,
// sin re-firmar ni re-marshalizar.
package retry

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DoomsDV/firmador-e/internal/kude"
	"github.com/DoomsDV/firmador-e/internal/sifen"
	"github.com/DoomsDV/firmador-e/internal/tenant"
)

// Config del worker.
type Config struct {
	Interval time.Duration // ESIGN_RETRY_INTERVAL (default 60s)
	MaxRetry int           // ESIGN_RETRY_MAX intentos por documento (default 10)
	Batch    int           // docs por ciclo (default 25)
	Enabled  bool
	// AllowProdWrites mantiene el worker sin reintentos fiscales en PROD por defecto.
	AllowProdWrites bool
	LeaseSeconds    int // lease del claim ORDS (default 360)
}

type sifenRetryClient interface {
	ConsultarDE(context.Context, int64, string, string) (int, []byte, error)
	RecibirDESync(context.Context, int64, []byte) (int, []byte, error)
}

// Worker consulta ORDS por pendientes y reenvía el XML firmado a SIFEN.
type Worker struct {
	resolver      *tenant.Resolver
	cfg           Config
	wait          func(context.Context, time.Duration) error
	clientFactory func(context.Context, int, sifen.Environment) (sifenRetryClient, error)
}

// New crea el worker. Si Enabled=false, Start es no-op.
func New(resolver *tenant.Resolver, cfg Config) *Worker {
	if cfg.Interval <= 0 {
		cfg.Interval = 60 * time.Second
	}
	if cfg.MaxRetry <= 0 {
		cfg.MaxRetry = 10
	}
	if cfg.Batch <= 0 {
		cfg.Batch = 25
	}
	if cfg.LeaseSeconds <= 0 {
		cfg.LeaseSeconds = 360
	}
	w := &Worker{
		resolver: resolver,
		cfg:      cfg,
		wait: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		},
	}
	w.clientFactory = func(ctx context.Context, clientID int, env sifen.Environment) (sifenRetryClient, error) {
		cert, err := resolver.GetCertificateByClientID(ctx, clientID)
		if err != nil {
			return nil, err
		}
		return sifen.NewClientFromP12Bytes(env, cert.P12, cert.Password, sifen.EndpointsFor(env).WsSync)
	}
	return w
}

// Start lanza el loop hasta que ctx se cancele.
func (w *Worker) Start(ctx context.Context) {
	if !w.cfg.Enabled {
		log.Printf("ℹ️  retry worker desactivado (ESIGN_RETRY_ENABLED=false)")
		return
	}
	log.Printf("🔁 retry worker activo (intervalo=%s, max=%d, batch=%d)", w.cfg.Interval, w.cfg.MaxRetry, w.cfg.Batch)
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()

	w.cycle(ctx)
	for {
		select {
		case <-ctx.Done():
			log.Printf("🔁 retry worker detenido")
			return
		case <-ticker.C:
			w.cycle(ctx)
		}
	}
}

func (w *Worker) cycle(ctx context.Context) {
	owner := fmt.Sprintf("retry-%d-%d", os.Getpid(), time.Now().UnixNano())
	docs, err := w.resolver.ORDS().ClaimPendingRetry(ctx, w.cfg.Batch, owner, w.cfg.LeaseSeconds, w.cfg.MaxRetry, w.cfg.AllowProdWrites)
	if err != nil {
		log.Printf("⚠️  retry: reclamar pendientes: %v", err)
		return
	}
	for i := range docs {
		if ctx.Err() != nil {
			return
		}
		if err := w.retryOne(ctx, &docs[i], owner); err != nil {
			log.Printf("⚠️  retry cdc=%s: %v", docs[i].CDC, err)
		}
	}
}

func (w *Worker) retryOne(ctx context.Context, doc *tenant.PendingRetryDoc, owner string) error {
	if strings.TrimSpace(doc.XMLFirmado) == "" {
		return w.requireManualRecovery(ctx, doc, fmt.Errorf("sin xml_firmado"))
	}
	if strings.TrimSpace(doc.XMLSHA256) == "" {
		return w.requireManualRecovery(ctx, doc, fmt.Errorf("sin xml_sha256"))
	}
	sum := sha256.Sum256([]byte(doc.XMLFirmado))
	if !strings.EqualFold(fmt.Sprintf("%x", sum[:]), doc.XMLSHA256) {
		return w.requireManualRecovery(ctx, doc, fmt.Errorf("xml_firmado no coincide con xml_sha256"))
	}

	env, err := sifen.ParseEnvironment(doc.Environment)
	if err != nil {
		return w.failRetry(ctx, doc, owner, fmt.Errorf("ambiente: %w", err))
	}
	if env == sifen.EnvProd && !w.cfg.AllowProdWrites {
		return w.failRetry(ctx, doc, owner, fmt.Errorf("reintento PROD bloqueado (ESIGN_SIFEN_PROD_WRITES_ENABLED=false)"))
	}
	if env != sifen.EnvTest && env != sifen.EnvProd {
		return w.failRetry(ctx, doc, owner, fmt.Errorf("ambiente de reenvio no soportado: %s", env))
	}

	if w.clientFactory == nil {
		return w.failRetry(ctx, doc, owner, fmt.Errorf("cliente SIFEN no configurado"))
	}
	client, err := w.clientFactory(ctx, doc.ClientID, env)
	if err != nil {
		return w.failRetry(ctx, doc, owner, fmt.Errorf("cliente WS: %w", err))
	}

	envioID, _ := strconv.ParseInt(strings.TrimLeft(doc.NumDocumento, "0"), 10, 64)
	if envioID == 0 {
		envioID = time.Now().Unix() % 1_000_000
	}

	// Nunca reenvíes a ciegas: un timeout puede haber sido aceptado por SIFEN.
	query := func() (sifen.ConsultaResult, error) {
		queryCtx, cancelQuery := context.WithTimeout(ctx, 60*time.Second)
		defer cancelQuery()
		_, queryBody, queryErr := client.ConsultarDE(queryCtx, envioID, sifen.EndpointsFor(env).WsConsulta, doc.CDC)
		if queryErr != nil {
			return sifen.ConsultaResult{}, queryErr
		}
		return sifen.ParseConsultaResult(queryBody), nil
	}

	consulta, err := query()
	if err != nil {
		return w.failRetry(ctx, doc, owner, fmt.Errorf("consultar antes de reenviar: %w", err))
	}
	if consulta.Found {
		remoteState := "APROBADO"
		if consulta.Cancelado {
			remoteState = "CANCELADO"
		}
		if _, err := w.resolver.ORDS().ReconcileDocument(ctx, doc.ClientID, doc.CDC, strings.ToUpper(doc.Environment), remoteState, consulta.CodRes, consulta.ProtAut, consulta.MsgRes, owner); err != nil {
			return w.failRetry(ctx, doc, owner, fmt.Errorf("persistir consulta SIFEN: %w", err))
		}
		log.Printf("✅ retry cdc=%s ya existe en SIFEN → %s", doc.CDC, remoteState)
		if remoteState == "APROBADO" {
			if err := w.enqueueKude(ctx, doc); err != nil {
				log.Printf("⚠️  retry cdc=%s: encolar KuDE: %v", doc.CDC, err)
			}
		}
		return nil
	}
	if consulta.CodRes != "0420" {
		return w.failRetry(ctx, doc, owner, fmt.Errorf("consulta SIFEN no autoritativa: %s %s", consulta.CodRes, consulta.MsgRes))
	}
	if env == sifen.EnvProd {
		for _, delay := range []time.Duration{5 * time.Second, 15 * time.Second} {
			if err := w.waitFor(ctx, delay); err != nil {
				return w.failRetry(ctx, doc, owner, fmt.Errorf("esperar confirmación PROD: %w", err))
			}
			consulta, err = query()
			if err != nil {
				return w.failRetry(ctx, doc, owner, fmt.Errorf("consultar confirmación PROD: %w", err))
			}
			if consulta.Found {
				remoteState := "APROBADO"
				if consulta.Cancelado {
					remoteState = "CANCELADO"
				}
				if _, err := w.resolver.ORDS().ReconcileDocument(ctx, doc.ClientID, doc.CDC, strings.ToUpper(doc.Environment), remoteState, consulta.CodRes, consulta.ProtAut, consulta.MsgRes, owner); err != nil {
					return w.failRetry(ctx, doc, owner, fmt.Errorf("persistir consulta SIFEN: %w", err))
				}
				return nil
			}
			if consulta.CodRes != "0420" {
				return w.failRetry(ctx, doc, owner, fmt.Errorf("confirmación PROD no autoritativa: %s %s", consulta.CodRes, consulta.MsgRes))
			}
		}
	}

	sendCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	_, body, err := client.RecibirDESync(sendCtx, envioID, []byte(doc.XMLFirmado))
	if err != nil {
		return w.failRetry(ctx, doc, owner, fmt.Errorf("enviar: %w", err))
	}

	res := sifen.ParseResult(body)
	estado, definitivo := sifen.EstadoDEResult(res.CodRes)
	if !definitivo {
		return w.failRetry(ctx, doc, owner, fmt.Errorf("respuesta de reenvío sin resultado fiscal: %s", res.MsgRes))
	}
	if err := w.persist(ctx, doc, res, estado, owner); err != nil {
		return w.failRetry(ctx, doc, owner, fmt.Errorf("persistir: %w", err))
	}
	log.Printf("✅ retry cdc=%s → %s (%s)", doc.CDC, estado, res.CodRes)

	if estado == "APROBADO" {
		if err := w.enqueueKude(ctx, doc); err != nil {
			log.Printf("⚠️  retry cdc=%s: encolar KuDE: %v", doc.CDC, err)
		}
	}
	return nil
}

func (w *Worker) waitFor(ctx context.Context, d time.Duration) error {
	if w.wait != nil {
		return w.wait(ctx, d)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

func (w *Worker) failRetry(ctx context.Context, doc *tenant.PendingRetryDoc, owner string, cause error) error {
	if err := w.resolver.ORDS().RecordRetryFailure(ctx, doc.ClientID, doc.CDC, owner, cause.Error(), w.cfg.MaxRetry); err != nil {
		return fmt.Errorf("%s; registrar fallo de retry: %w", cause, err)
	}
	return cause
}

func (w *Worker) requireManualRecovery(ctx context.Context, doc *tenant.PendingRetryDoc, cause error) error {
	if err := w.resolver.ORDS().MarkRetryReconciliationRequired(ctx, doc.ClientID, doc.CDC, cause.Error()); err != nil {
		return fmt.Errorf("%s; marcar recuperación manual: %w", cause, err)
	}
	return cause
}

func (w *Worker) persist(ctx context.Context, doc *tenant.PendingRetryDoc, res sifen.Result, estado string, owners ...string) error {
	total := ""
	if doc.TotalOperacion != nil {
		total = strconv.FormatFloat(*doc.TotalOperacion, 'f', -1, 64)
	}
	owner := ""
	if len(owners) > 0 {
		owner = owners[0]
	}
	return w.resolver.ORDS().RegisterDocument(ctx, tenant.DocumentRecord{
		ClientID:        doc.ClientID,
		Environment:     strings.ToUpper(doc.Environment),
		TipoDE:          doc.TipoDE,
		CDC:             doc.CDC,
		NumeroDoc:       doc.NumDocumento,
		Establecimiento: doc.Establecimiento,
		PuntoExpedicion: doc.PuntoExpedicion,
		ReceptorNombre:  doc.ReceptorNombre,
		ReceptorDoc:     doc.ReceptorDoc,
		Moneda:          doc.Moneda,
		TotalOperacion:  total,
		Estado:          estado,
		CodRes:          res.CodRes,
		ProtAut:         res.ProtAut,
		MensajeRes:      res.MsgRes,
		XMLFirmado:      doc.XMLFirmado,
		QRURL:           doc.QRURL,
		FromRetry:       true,
		RetryLeaseOwner: owner,
		IdempotencyKey:  doc.IdempotencyKey,
	})
}

// enqueueKude reconstruye KudeData desde el XML firmado canónico y encola la
// tarea durable (misma cola que la emisión síncrona).
func (w *Worker) enqueueKude(ctx context.Context, doc *tenant.PendingRetryDoc) error {
	rde, err := sifen.ParseRDEXML([]byte(doc.XMLFirmado))
	if err != nil {
		return fmt.Errorf("parsear xml: %w", err)
	}
	branding := kude.Branding{TemplateID: kude.TemplateMinimalista, MostrarFantasia: true}
	if cfg, err := w.resolver.ORDS().GetKudeConfig(ctx, doc.ClientID); err == nil {
		branding = kude.Branding{
			TemplateID:      cfg.TemplateID,
			ColorPrimario:   cfg.ColorPrimario,
			LogoURL:         cfg.LogoURL,
			NotasFooter:     cfg.NotasFooter,
			MostrarFantasia: cfg.MostrarFantasia,
		}
	}
	qrURL := doc.QRURL
	if qrURL == "" {
		return fmt.Errorf("sin qr_url")
	}
	data, err := kude.BuildKudeData(rde, qrURL, strings.ToUpper(doc.Environment), branding)
	if err != nil {
		return fmt.Errorf("build kude: %w", err)
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("serializar payload: %w", err)
	}
	return w.resolver.ORDS().EnqueueKudeTask(ctx, doc.ClientID, doc.CDC, string(payload))
}
