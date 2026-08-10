package sifen

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const opensslTimeout = 30 * time.Second

// OpenPKCS12Result holds the parsed certificate and the PKCS#12 bytes to persist.
// StoredBytes is normalized via OpenSSL when the original DNIT/Marandú export is not
// readable by go-pkcs12 (common with identity.p12 from Paraguay).
type OpenPKCS12Result struct {
	Cert        *DigitalCertificate
	StoredBytes []byte
}

// OpenPKCS12 opens a PKCS#12 with password. If go-pkcs12 cannot decode the file
// (e.g. DNIT original export), it transparently re-encodes via OpenSSL and retries.
// StoredBytes should be encrypted and saved — usually the normalized form.
func OpenPKCS12(p12Data []byte, password string) (*OpenPKCS12Result, error) {
	if len(p12Data) == 0 {
		return nil, fmt.Errorf("p12 vacío")
	}
	if err := ValidateP12DER(p12Data); err != nil {
		return nil, err
	}

	cert, err := decodePKCS12(p12Data, password)
	if err == nil {
		return &OpenPKCS12Result{Cert: cert, StoredBytes: p12Data}, nil
	}

	normalized, normErr := normalizePKCS12OpenSSL(p12Data, password)
	if normErr != nil {
		return nil, formatP12LoadError(err)
	}
	cert, err = decodePKCS12(normalized, password)
	if err != nil {
		return nil, formatP12LoadError(err)
	}
	return &OpenPKCS12Result{Cert: cert, StoredBytes: normalized}, nil
}

func normalizePKCS12OpenSSL(p12Data []byte, password string) ([]byte, error) {
	openssl, err := opensslPath()
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "esign-p12-")
	if err != nil {
		return nil, fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	inPath := filepath.Join(dir, "in.p12")
	pemPath := filepath.Join(dir, "bundle.pem")
	outPath := filepath.Join(dir, "out.p12")

	if err := os.WriteFile(inPath, p12Data, 0o600); err != nil {
		return nil, fmt.Errorf("escribir p12 temp: %w", err)
	}

	if err := runOpenSSL(openssl, []string{
		"pkcs12", "-in", inPath, "-passin", "stdin", "-nodes", "-out", pemPath,
	}, password); err != nil {
		return nil, fmt.Errorf("openssl pkcs12 extract: %w", err)
	}
	if err := runOpenSSL(openssl, []string{
		"pkcs12", "-export", "-in", pemPath, "-passout", "stdin", "-out", outPath,
	}, password); err != nil {
		return nil, fmt.Errorf("openssl pkcs12 export: %w", err)
	}

	out, err := os.ReadFile(outPath)
	if err != nil {
		return nil, fmt.Errorf("leer p12 normalizado: %w", err)
	}
	if err := ValidateP12DER(out); err != nil {
		return nil, fmt.Errorf("p12 normalizado inválido: %w", err)
	}
	return out, nil
}

func runOpenSSL(bin string, args []string, password string) error {
	ctx, cancel := context.WithTimeout(context.Background(), opensslTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = strings.NewReader(password)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func opensslPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv("ESIGN_OPENSSL_PATH")); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("ESIGN_OPENSSL_PATH no encontrado: %s", p)
	}
	if path, err := exec.LookPath("openssl"); err == nil {
		return path, nil
	}
	if runtime.GOOS == "windows" {
		candidates := []string{
			`C:\Program Files\Git\usr\bin\openssl.exe`,
			`C:\Program Files\Git\mingw64\bin\openssl.exe`,
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				return c, nil
			}
		}
	}
	return "", fmt.Errorf("openssl no encontrado en PATH (instalar OpenSSL o setear ESIGN_OPENSSL_PATH)")
}
