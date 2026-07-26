package tenant

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func TestEncryptDecryptRoundtrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("gen key: %v", err)
	}
	plaintext := []byte("ABCD0000000000000000000000000000") // un CSC de prueba

	nonce, ct, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if len(nonce) != nonceSize {
		t.Fatalf("nonce esperado %d bytes, got %d", nonceSize, len(nonce))
	}

	got, err := Decrypt(key, nonce, ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("roundtrip: got %q want %q", got, plaintext)
	}
}

func TestDecryptHexMatchesEncrypt(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("gen key: %v", err)
	}
	plaintext := []byte("secreto-p12-password")
	nonce, ct, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	got, err := DecryptHex(key, hex.EncodeToString(nonce), hex.EncodeToString(ct))
	if err != nil {
		t.Fatalf("decryptHex: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("got %q want %q", got, plaintext)
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	nonce, ct, err := Encrypt(key, []byte("hola"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	other := make([]byte, 32)
	_, _ = rand.Read(other)
	if _, err := Decrypt(other, nonce, ct); err == nil {
		t.Fatal("descifrar con clave equivocada debería fallar (auth GCM)")
	}
}
