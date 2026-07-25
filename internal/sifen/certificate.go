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
