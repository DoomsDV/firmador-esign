package sifen

import (
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"

	"software.sslmate.com/src/go-pkcs12"
)

const minP12DERSize = 256

type DigitalCertificate struct {
	PrivateKey  *rsa.PrivateKey
	Certificate *x509.Certificate
}

// LoadCertificateFromFile lee un PKCS#12 (.p12) del disco y extrae clave RSA + certificado.
func LoadCertificateFromFile(path, password string) (*DigitalCertificate, error) {
	p12Data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no se pudo leer el archivo p12 %q: %w", path, err)
	}
	return LoadCertificateFromBytes(p12Data, password)
}

// ValidateP12DER comprueba tamaño mínimo y cabecera ASN.1 DER antes de decodificar.
func ValidateP12DER(p12Data []byte) error {
	if len(p12Data) < minP12DERSize {
		return fmt.Errorf("archivo demasiado pequeño para ser un .p12 válido")
	}
	if p12Data[0] != 0x30 {
		return fmt.Errorf("formato inválido; debe ser .p12/.pfx DER, no PEM ni otro archivo")
	}
	return nil
}

// LoadCertificateFromBytes extrae clave RSA + certificado de un PKCS#12 ya en
// memoria (usado en multi-tenant: el .p12 llega descifrado, nunca desde disco).
func LoadCertificateFromBytes(p12Data []byte, password string) (*DigitalCertificate, error) {
	if len(p12Data) == 0 {
		return nil, fmt.Errorf("p12 vacío")
	}
	if err := ValidateP12DER(p12Data); err != nil {
		return nil, err
	}
	digital, err := decodePKCS12(p12Data, password)
	if err != nil {
		return nil, formatP12LoadError(err)
	}
	return digital, nil
}

func decodePKCS12(p12Data []byte, password string) (*DigitalCertificate, error) {
	var lastErr error

	if digital, err := decodeWithChain(p12Data, password); err == nil {
		return digital, nil
	} else {
		lastErr = err
	}

	if digital, err := decodeWithDecode(p12Data, password); err == nil {
		return digital, nil
	} else {
		lastErr = err
	}

	if digital, err := decodeWithPEM(p12Data, password); err == nil {
		return digital, nil
	} else {
		lastErr = err
	}

	return nil, lastErr
}

func decodeWithChain(p12Data []byte, password string) (*DigitalCertificate, error) {
	key, certificate, _, err := pkcs12.DecodeChain(p12Data, password)
	if err != nil {
		return nil, err
	}
	return digitalFromKeyCert(key, certificate)
}

func decodeWithDecode(p12Data []byte, password string) (*DigitalCertificate, error) {
	key, certificate, err := pkcs12.Decode(p12Data, password)
	if err != nil {
		return nil, err
	}
	return digitalFromKeyCert(key, certificate)
}

func decodeWithPEM(p12Data []byte, password string) (*DigitalCertificate, error) {
	blocks, err := pkcs12.ToPEM(p12Data, password)
	if err != nil {
		return nil, err
	}
	var certificate *x509.Certificate
	var rsaKey *rsa.PrivateKey
	for _, block := range blocks {
		switch block.Type {
		case "CERTIFICATE":
			c, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, err
			}
			if certificate == nil {
				certificate = c
			}
		case "PRIVATE KEY":
			key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return nil, err
			}
			rsaKey, err = asRSAKey(key)
			if err != nil {
				return nil, err
			}
		case "RSA PRIVATE KEY":
			key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
			if err != nil {
				return nil, err
			}
			rsaKey = key
		}
	}
	if certificate == nil || rsaKey == nil {
		return nil, errors.New("pkcs12: private key missing")
	}
	return &DigitalCertificate{PrivateKey: rsaKey, Certificate: certificate}, nil
}

func digitalFromKeyCert(key any, certificate *x509.Certificate) (*DigitalCertificate, error) {
	if certificate == nil {
		return nil, errors.New("pkcs12: certificate missing")
	}
	rsaKey, err := asRSAKey(key)
	if err != nil {
		return nil, err
	}
	return &DigitalCertificate{PrivateKey: rsaKey, Certificate: certificate}, nil
}

func asRSAKey(key any) (*rsa.PrivateKey, error) {
	if key == nil {
		return nil, errors.New("pkcs12: private key missing")
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("la llave privada no es RSA")
	}
	return rsaKey, nil
}

func formatP12LoadError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()

	if errors.Is(err, pkcs12.ErrDecryption) || errors.Is(err, pkcs12.ErrIncorrectPassword) {
		return fmt.Errorf("contraseña incorrecta o archivo .p12 dañado")
	}
	if strings.Contains(msg, "la llave privada no es RSA") {
		return fmt.Errorf("se requiere certificado RSA (DNIT/SIFEN)")
	}
	if strings.Contains(msg, "empty encrypted data") ||
		strings.Contains(msg, "private key missing") ||
		strings.Contains(msg, "certificate missing") {
		return fmt.Errorf("el .p12 no contiene una clave privada de firma válida; verificá que sea el certificado de firma electrónica (no solo identidad) exportado como PKCS#12 con clave privada")
	}
	return fmt.Errorf("error decodificando p12 (revisar contraseña/formato): %w", err)
}
