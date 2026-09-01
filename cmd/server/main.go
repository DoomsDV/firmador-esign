// Command server expone el motor de firma SIFEN como API HTTP multi-tenant.
// El ambiente (test/prod) lo determina el prefijo de la API key (sk_test_/sk_prod_).
// La configuración del tenant (emisor, timbrado, CSC, certificado) se resuelve
// contra ORDS; los secretos se descifran en memoria con ESIGN_MASTER_KEY (AES-GCM).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/DoomsDV/firmador-e/internal/gotenberg"
	"github.com/DoomsDV/firmador-e/internal/httpapi"
	"github.com/DoomsDV/firmador-e/internal/kudequeue"
	"github.com/DoomsDV/firmador-e/internal/retry"
	"github.com/DoomsDV/firmador-e/internal/tenant"
)

func main() {
	_ = godotenv.Load()

	ordsBase := mustEnv("ESIGN_ORDS_BASE")
	serviceToken := mustEnv("ESIGN_SERVICE_TOKEN")

	masterKey, err := tenant.LoadMasterKey()
	if err != nil {
		log.Fatalf("❌ %v", err)
	}

	ttl := parseTTL(os.Getenv("ESIGN_TENANT_CACHE_TTL"), 5*time.Minute)

	ords := tenant.NewORDSClient(ordsBase, serviceToken)
	resolver := tenant.NewResolver(ords, masterKey, ttl)

	// ESIGN_JWT_SECRET = valor exacto de app_parameter.JWT_TOKEN (bytes ASCII del string).
	jwtSecret := []byte(strings.TrimSpace(os.Getenv("ESIGN_JWT_SECRET")))
	if len(jwtSecret) == 0 {
		log.Printf("⚠️  ESIGN_JWT_SECRET vacio: /v1/panel/* devolvera 503")
	}

	gotenbergURL := envOr("GOTENBERG_URL", "http://localhost:3000")
	kudeTimeout := parseTTL(os.Getenv("ESIGN_KUDE_TIMEOUT"), 20*time.Second)

	srv := httpapi.New(resolver, httpapi.ServerOptions{
		JWTSecret:    jwtSecret,
		JWTIssuer:    envOr("ESIGN_JWT_ISSUER", "esign-api"),
		JWTAudience:  envOr("ESIGN_JWT_AUDIENCE", "esign-app"),
		GotenbergURL: gotenbergURL,
		KudeTimeout:  kudeTimeout,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	worker := retry.New(resolver, retry.Config{
		Interval: parseTTL(os.Getenv("ESIGN_RETRY_INTERVAL"), 60*time.Second),
		MaxRetry: envInt("ESIGN_RETRY_MAX", 10),
		Batch:    envInt("ESIGN_RETRY_BATCH", 25),
		Enabled:  envBool("ESIGN_RETRY_ENABLED", true),
	})
	go worker.Start(ctx)

	kudeWorker := kudequeue.New(resolver, gotenberg.New(gotenbergURL, kudeTimeout), kudequeue.Config{
		Interval:     parseTTL(os.Getenv("ESIGN_KUDE_QUEUE_INTERVAL"), 15*time.Second),
		Batch:        envInt("ESIGN_KUDE_QUEUE_BATCH", 10),
		LeaseSeconds: envInt("ESIGN_KUDE_QUEUE_LEASE", 120),
		Timeout:      kudeTimeout,
		Enabled:      envBool("ESIGN_KUDE_QUEUE_ENABLED", true),
	})
	go kudeWorker.Start(ctx)

	addr := envOr("SERVER_ADDR", ":8080")
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      90 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("🚀 esign API escuchando en %s (ORDS=%s, cache TTL=%s)", addr, ordsBase, ttl)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("❌ servidor: %v", err)
	}
}

func mustEnv(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		log.Fatalf("❌ falta la variable de entorno %s", key)
	}
	return v
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("⚠️  %s invalido (%q): usando %d", key, raw, def)
		return def
	}
	return n
}

func envBool(key string, def bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if raw == "" {
		return def
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		log.Printf("⚠️  %s invalido (%q): usando %v", key, raw, def)
		return def
	}
}

func parseTTL(raw string, def time.Duration) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		log.Printf("⚠️  duration invalida (%q): usando %s", raw, def)
		return def
	}
	return d
}
