package sifen

import (
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"os"

	"software.sslmate.com/src/go-pkcs12"
)

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

// LoadCertificateFromBytes extrae clave RSA + certificado de un PKCS#12 ya en
// memoria (usado en multi-tenant: el .p12 llega descifrado, nunca desde disco).
func LoadCertificateFromBytes(p12Data []byte, password string) (*DigitalCertificate, error) {
	if len(p12Data) == 0 {
		return nil, fmt.Errorf("p12 vacío")
	}
	key, certificate, _, err := pkcs12.DecodeChain(p12Data, password)
	if err != nil {
		return nil, fmt.Errorf("error decodificando p12 (revisar contraseña/formato): %w", err)
	}

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("la llave privada no es RSA")
	}

	return &DigitalCertificate{
		PrivateKey:  rsaKey,
		Certificate: certificate,
	}, nil
}
