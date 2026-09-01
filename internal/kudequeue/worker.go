// Package kudequeue implementa el worker que reclama tareas durables de
// generación de KuDE (PDF vía Gotenberg) y las completa con UploadKude.
package kudequeue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/DoomsDV/firmador-e/internal/gotenberg"
	"github.com/DoomsDV/firmador-e/internal/kude"
	"github.com/DoomsDV/firmador-e/internal/sifen"
	"github.com/DoomsDV/firmador-e/internal/tenant"
)

// Config del worker de cola KuDE.
type Config struct {
	Interval     time.Duration
	Batch        int
	LeaseSeconds int
	Enabled      bool
	Timeout      time.Duration
}

// Worker reclama document_kude_task y renderiza/sube el PDF.
type Worker struct {
	resolver  *tenant.Resolver
	gotenberg *gotenberg.Client
	cfg       Config
	owner     string
}

// New crea el worker. Si Enabled=false, Start es no-op.
func New(resolver *tenant.Resolver, g *gotenberg.Client, cfg Config) *Worker {
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
		cfg.Timeout = gotenberg.DefaultTimeout
	}
	owner, _ := os.Hostname()
	if owner == "" {
		owner = "kude-worker"
	}
	return &Worker{resolver: resolver, gotenberg: g, cfg: cfg, owner: owner}
}

// Start lanza el loop hasta que ctx se cancele.
func (w *Worker) Start(ctx context.Context) {
	if !w.cfg.Enabled {
		log.Printf("ℹ️  kude queue worker desactivado (ESIGN_KUDE_QUEUE_ENABLED=false)")
		return
	}
	log.Printf("🧾 kude queue worker activo (intervalo=%s, batch=%d, lease=%ds)",
		w.cfg.Interval, w.cfg.Batch, w.cfg.LeaseSeconds)
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()

	w.cycle(ctx)
	for {
		select {
		case <-ctx.Done():
			log.Printf("🧾 kude queue worker detenido")
			return
		case <-ticker.C:
			w.cycle(ctx)
		}
	}
}

func (w *Worker) cycle(ctx context.Context) {
	w.reconcileMissingTasks(ctx)

	tasks, err := w.resolver.ORDS().ClaimKudeTasks(ctx, w.owner, w.cfg.LeaseSeconds, w.cfg.Batch)
	if err != nil {
		log.Printf("⚠️  kude queue: claim: %v", err)
		return
	}
	for i := range tasks {
		if ctx.Err() != nil {
			return
		}
		if err := w.processOne(ctx, &tasks[i]); err != nil {
			log.Printf("⚠️  kude queue task=%d cdc=%s: %v", tasks[i].TaskID, tasks[i].CDC, err)
		}
	}
}

// reconcileMissingTasks convierte en tareas durables los documentos APROBADO
// cuyo proceso murió después de persistir el XML, pero antes de encolar KuDE.
func (w *Worker) reconcileMissingTasks(ctx context.Context) {
	docs, err := w.resolver.ORDS().ListKudeRecoveryDocuments(ctx, w.cfg.Batch)
	if err != nil {
		log.Printf("⚠️  kude queue: recuperar tareas faltantes: %v", err)
		return
	}

	for i := range docs {
		if ctx.Err() != nil {
			return
		}
		doc := &docs[i]
		if err := w.enqueueRecoveredDocument(ctx, doc); err != nil {
			log.Printf("⚠️  kude queue recovery cdc=%s: %v", doc.CDC, err)
		}
	}
}

func (w *Worker) enqueueRecoveredDocument(ctx context.Context, doc *tenant.KudeRecoveryDocument) error {
	if strings.TrimSpace(doc.XMLFirmado) == "" || strings.TrimSpace(doc.QRURL) == "" {
		return fmt.Errorf("xml o QR faltante")
	}

	rde, err := sifen.ParseRDEXML([]byte(doc.XMLFirmado))
	if err != nil {
		return fmt.Errorf("parsear xml firmado: %w", err)
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
	data, err := kude.BuildKudeData(rde, doc.QRURL, strings.ToUpper(doc.Environment), branding)
	if err != nil {
		return fmt.Errorf("armar KuDE: %w", err)
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("serializar KuDE: %w", err)
	}
	if err := w.resolver.ORDS().EnqueueKudeTask(ctx, doc.ClientID, doc.CDC, string(payload)); err != nil {
		return fmt.Errorf("encolar tarea: %w", err)
	}
	log.Printf("🧾 kude queue recovery cdc=%s → PENDING", doc.CDC)
	return nil
}

func (w *Worker) processOne(ctx context.Context, task *tenant.KudeTask) error {
	if task.PayloadJSON == "" {
		msg := "payload_json vacío"
		_ = w.resolver.ORDS().CompleteKudeTask(ctx, task.TaskID, false, msg)
		return fmt.Errorf("%s", msg)
	}

	var data kude.KudeData
	if err := json.Unmarshal([]byte(task.PayloadJSON), &data); err != nil {
		msg := "payload_json inválido: " + err.Error()
		_ = w.resolver.ORDS().CompleteKudeTask(ctx, task.TaskID, false, msg)
		return fmt.Errorf("%s", msg)
	}

	runCtx, cancel := context.WithTimeout(ctx, w.cfg.Timeout)
	defer cancel()

	html, err := kude.RenderKuDEHTML(data)
	if err != nil {
		msg := "render HTML: " + err.Error()
		_ = w.resolver.ORDS().CompleteKudeTask(ctx, task.TaskID, false, msg)
		return fmt.Errorf("%s", msg)
	}
	pdf, err := w.gotenberg.Render(runCtx, html)
	if err != nil {
		msg := "gotenberg: " + err.Error()
		_ = w.resolver.ORDS().CompleteKudeTask(ctx, task.TaskID, false, msg)
		return fmt.Errorf("%s", msg)
	}
	if _, err := w.resolver.ORDS().UploadKude(runCtx, task.ClientID, task.CDC, pdf); err != nil {
		msg := "upload: " + err.Error()
		_ = w.resolver.ORDS().CompleteKudeTask(ctx, task.TaskID, false, msg)
		return fmt.Errorf("%s", msg)
	}
	if err := w.resolver.ORDS().CompleteKudeTask(ctx, task.TaskID, true, ""); err != nil {
		return fmt.Errorf("complete READY: %w", err)
	}
	log.Printf("✅ kude queue cdc=%s → READY", task.CDC)
	return nil
}
