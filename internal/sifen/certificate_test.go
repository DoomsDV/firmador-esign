package sifen

import (
	"errors"
	"strings"
	"testing"

	"software.sslmate.com/src/go-pkcs12"
)

func TestValidateP12DER(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{
			name:    "too small",
			data:    make([]byte, 100),
			wantErr: "demasiado pequeño",
		},
		{
			name:    "not der sequence",
			data:    append(make([]byte, minP12DERSize), 0x31),
			wantErr: "formato inválido",
		},
		{
			name: "valid header",
			data: func() []byte {
				b := make([]byte, minP12DERSize)
				b[0] = 0x30
				return b
			}(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateP12DER(tt.data)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got err=%v want contains %q", err, tt.wantErr)
			}
		})
	}
}

func TestFormatP12LoadError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		err        error
		wantSubstr string
	}{
		{
			name:       "wrong password",
			err:        pkcs12.ErrIncorrectPassword,
			wantSubstr: "contraseña incorrecta",
		},
		{
			name:       "decryption padding",
			err:        pkcs12.ErrDecryption,
			wantSubstr: "contraseña incorrecta",
		},
		{
			name:       "empty encrypted data",
			err:        errors.New("pkcs12: empty encrypted data"),
			wantSubstr: "clave privada de firma válida",
		},
		{
			name:       "private key missing",
			err:        errors.New("pkcs12: private key missing"),
			wantSubstr: "clave privada de firma válida",
		},
		{
			name:       "non rsa",
			err:        errors.New("la llave privada no es RSA"),
			wantSubstr: "certificado RSA",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatP12LoadError(tt.err)
			if got == nil || !strings.Contains(got.Error(), tt.wantSubstr) {
				t.Fatalf("got=%v want contains %q", got, tt.wantSubstr)
			}
		})
	}
}

func TestLoadCertificateFromBytes_rejectsInvalidDER(t *testing.T) {
	t.Parallel()
	_, err := LoadCertificateFromBytes([]byte{0x31, 0x02}, "secret")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "demasiado pequeño") {
		t.Fatalf("got %v", err)
	}
}
