// Package tenant resuelve el contexto multi-tenant (cliente + configuración SIFEN
// + secretos cifrados) contra ORDS y descifra en memoria los materiales sensibles
// (certificado .p12 y CSC) con la clave maestra de la aplicación. Oracle nunca ve
// el material en claro: solo almacena ciphertext + nonce opacos.
package tenant

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// nonceSize es el tamaño del nonce GCM (estándar 12 bytes). Se almacena en las
// columnas RAW(16) de Oracle (caben holgados).
const nonceSize = 12

// masterKeyEnv es la variable de entorno con la clave maestra AES-256 (64 hex = 32 bytes).
const masterKeyEnv = "ESIGN_MASTER_KEY"

// LoadMasterKey lee y valida la clave maestra AES-256 desde ESIGN_MASTER_KEY.
func LoadMasterKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(masterKeyEnv))
	if raw == "" {
		return nil, fmt.Errorf("falta %s (clave maestra AES-256)", masterKeyEnv)
	}
	return DeriveMasterKey(raw)
}

// DeriveMasterKey normaliza la clave maestra a 32 bytes admitiendo tres formatos
// (en este orden): 64 caracteres hex, base64 de 32 bytes, o 32 caracteres crudos
// (se usan sus bytes UTF-8). El server y el seed la derivan igual, garantizando
// que lo cifrado por uno lo descifre el otro.
func DeriveMasterKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) == 64 {
		if b, err := hex.DecodeString(raw); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	if len(raw) == 32 {
		return []byte(raw), nil
	}
	return nil, fmt.Errorf("%s debe ser 64 hex, base64 de 32 bytes, o 32 caracteres; got %d caracteres", masterKeyEnv, len(raw))
}

// Encrypt cifra plaintext con AES-256-GCM y devuelve nonce y ciphertext (este
// último incluye el tag de autenticación GCM al final). Se usa al aprovisionar
// certificados/CSC (el ciphertext+nonce se guardan opacos en Oracle).
func Encrypt(key, plaintext []byte) (nonce, ciphertext []byte, err error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("generar nonce: %w", err)
	}
	ciphertext = gcm.Seal(nil, nonce, plaintext, nil)
	return nonce, ciphertext, nil
}

// Decrypt descifra ciphertext (con tag GCM incluido) usando key y nonce.
func Decrypt(key, nonce, ciphertext []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		// GCM tolera otros tamaños de nonce solo si se construyó con NonceSize
		// distinto; acá exigimos el estándar para descartar datos corruptos.
		if len(nonce) == 0 {
			return nil, fmt.Errorf("nonce vacío")
		}
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("descifrar (clave maestra/nonce/datos): %w", err)
	}
	return plaintext, nil
}

// DecryptHex descifra a partir de nonce y ciphertext en representación hex (tal
// como los devuelve ORDS desde los BLOB de Oracle).
func DecryptHex(key []byte, nonceHex, ciphertextHex string) ([]byte, error) {
	nonce, err := hex.DecodeString(strings.TrimSpace(nonceHex))
	if err != nil {
		return nil, fmt.Errorf("nonce hex inválido: %w", err)
	}
	ct, err := hex.DecodeString(strings.TrimSpace(ciphertextHex))
	if err != nil {
		return nil, fmt.Errorf("ciphertext hex inválido: %w", err)
	}
	return Decrypt(key, nonce, ct)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("clave maestra debe ser de 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crear cipher AES: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crear GCM: %w", err)
	}
	return gcm, nil
}
