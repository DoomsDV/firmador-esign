// Command server expone el motor de firma SIFEN como API HTTP multi-tenant.
// El ambiente (test/prod) lo determina el prefijo de la API key (sk_test_/sk_prod_).
// La configuración del tenant (emisor, timbrado, CSC, certificado) se resuelve
// contra ORDS; los secretos se descifran en memoria con ESIGN_MASTER_KEY (AES-GCM).
package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/DoomsDV/firmador-e/internal/httpapi"
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
	srv := httpapi.New(resolver)

	addr := envOr("SERVER_ADDR", ":8080")
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      90 * time.Second,
	}

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

func parseTTL(raw string, def time.Duration) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		log.Printf("⚠️  ESIGN_TENANT_CACHE_TTL inválido (%q): usando %s", raw, def)
		return def
	}
	return d
}
