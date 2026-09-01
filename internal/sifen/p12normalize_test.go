package sifen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joho/godotenv"
)

func TestOpenPKCS12_DNITOriginal(t *testing.T) {
	if _, err := opensslPath(); err != nil {
		t.Skip("openssl no disponible:", err)
	}
	_ = godotenv.Load(filepath.Join("..", "..", ".env"))
	pass := strings.TrimSpace(os.Getenv("CERT_P12_PASSWORD"))
	if pass == "" {
		t.Skip("CERT_P12_PASSWORD no configurado")
	}

	orig := filepath.Join(`C:\Users\HP\Documents\dnit-doc`, "7156160_identity.p12")
	if _, err := os.Stat(orig); err != nil {
		t.Skip("7156160_identity.p12 no presente en dev")
	}
	b, err := os.ReadFile(orig)
	if err != nil {
		t.Fatal(err)
	}

	res, err := OpenPKCS12(b, pass)
	if err != nil {
		t.Fatalf("OpenPKCS12: %v", err)
	}
	if res.Cert == nil || res.Cert.Certificate == nil {
		t.Fatal("cert nil")
	}
	if len(res.StoredBytes) == 0 {
		t.Fatal("stored bytes empty")
	}
	if _, err := decodePKCS12(res.StoredBytes, pass); err != nil {
		t.Fatalf("stored bytes not decodable: %v", err)
	}
}
