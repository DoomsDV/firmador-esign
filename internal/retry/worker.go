// Package retry implementa el worker que reenvía a SIFEN los documentos en estado
// FIRMADO (fallo transitorio de red). Regla 0141: reenvía el xml_firmado EXACTO,
// sin re-firmar ni re-marshalizar.
package retry

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/DoomsDV/firmador-e/internal/sifen"
	"github.com/DoomsDV/firmador-e/internal/tenant"
)

// Config del worker.
type Config struct {
	Interval time.Duration // ESIGN_RETRY_INTERVAL (default 60s)
	MaxRetry int           // ESIGN_RETRY_MAX intentos por documento (default 10)
	Batch    int           // docs por ciclo (default 25)
	Enabled  bool
}

// Worker consulta ORDS por pendientes y reenvía el XML firmado a SIFEN.
type Worker struct {
	resolver *tenant.Resolver
	cfg      Config
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
	return &Worker{resolver: resolver, cfg: cfg}
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
	docs, err := w.resolver.ORDS().ListPendingRetry(ctx, "flagged", w.cfg.Batch)
	if err != nil {
		log.Printf("⚠️  retry: listar flagged: %v", err)
		return
	}
	if len(docs) == 0 {
		docs, err = w.resolver.ORDS().ListPendingRetry(ctx, "all", w.cfg.Batch)
		if err != nil {
			log.Printf("⚠️  retry: listar all: %v", err)
			return
		}
	}
	for i := range docs {
		if ctx.Err() != nil {
			return
		}
		if err := w.retryOne(ctx, &docs[i]); err != nil {
			log.Printf("⚠️  retry cdc=%s: %v", docs[i].CDC, err)
		}
	}
}

func (w *Worker) retryOne(ctx context.Context, doc *tenant.PendingRetryDoc) error {
	if doc.RetryCount >= w.cfg.MaxRetry {
		return fmt.Errorf("supera ESIGN_RETRY_MAX (%d)", w.cfg.MaxRetry)
	}
	if strings.TrimSpace(doc.XMLFirmado) == "" {
		return fmt.Errorf("sin xml_firmado")
	}

	env, err := sifen.ParseEnvironment(doc.Environment)
	if err != nil {
		return fmt.Errorf("ambiente: %w", err)
	}
	if env != sifen.EnvTest {
		return fmt.Errorf("reenvio a %s bloqueado (solo TEST)", env)
	}

	cert, err := w.resolver.GetCertificateByClientID(ctx, doc.ClientID)
	if err != nil {
		return fmt.Errorf("certificado: %w", err)
	}

	syncURL := sifen.EndpointsFor(env).WsSync
	client, err := sifen.NewClientFromP12Bytes(env, cert.P12, cert.Password, syncURL)
	if err != nil {
		return fmt.Errorf("cliente WS: %w", err)
	}

	envioID, _ := strconv.ParseInt(strings.TrimLeft(doc.NumDocumento, "0"), 10, 64)
	if envioID == 0 {
		envioID = time.Now().Unix() % 1_000_000
	}

	sendCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	_, body, err := client.RecibirDESync(sendCtx, envioID, []byte(doc.XMLFirmado))
	if err != nil {
		_ = w.persist(ctx, doc, sifen.Result{}, "FIRMADO")
		return fmt.Errorf("enviar: %w", err)
	}

	res := sifen.ParseResult(body)
	estado := sifen.EstadoDE(res.CodRes)
	if err := w.persist(ctx, doc, res, estado); err != nil {
		return fmt.Errorf("persistir: %w", err)
	}
	log.Printf("✅ retry cdc=%s → %s (%s)", doc.CDC, estado, res.CodRes)
	return nil
}

func (w *Worker) persist(ctx context.Context, doc *tenant.PendingRetryDoc, res sifen.Result, estado string) error {
	total := ""
	if doc.TotalOperacion != nil {
		total = strconv.FormatFloat(*doc.TotalOperacion, 'f', -1, 64)
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
		FromRetry:       true,
	})
}
