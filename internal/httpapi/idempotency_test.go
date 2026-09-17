package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestCancelRequestFingerprintUsesNormalizedPayload(t *testing.T) {
	t.Parallel()

	got := hashCancelRequest(" 123 ", "  Documento\n   con datos incorrectos  ")
	wantBytes := sha256.Sum256([]byte("CANCELACION\n123\nDocumento con datos incorrectos"))
	want := hex.EncodeToString(wantBytes[:])
	if got != want {
		t.Fatalf("fingerprint = %q, want %q", got, want)
	}
	if got != hashCancelRequest("123", "Documento con datos incorrectos") {
		t.Fatal("equivalent normalized payloads must have the same fingerprint")
	}
}

func TestEventIDForSeparatesEnvironments(t *testing.T) {
	t.Parallel()

	testID := eventIDFor("TEST", "CANCELACION", "key-1")
	if testID != eventIDFor("TEST", "CANCELACION", "key-1") {
		t.Fatal("same environment, type and key must produce a stable event ID")
	}
	if testID == eventIDFor("PROD", "CANCELACION", "key-1") {
		t.Fatal("TEST and PROD must not share the deterministic event ID input")
	}
}

func TestNormalizeCancelMotivo(t *testing.T) {
	t.Parallel()

	if got := normalizeCancelMotivo("  una\trazón\ncon espacios "); got != "una razón con espacios" {
		t.Fatalf("normalized motive = %q", got)
	}
}
