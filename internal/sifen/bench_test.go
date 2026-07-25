package sifen

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"
)

// Benchmarks locales (sin red): miden BuildDE + firma Exc-C14N.
// No tocan la lógica de producción; usan certificado RSA efímero.

func benchCert(b *testing.B) *DigitalCertificate {
	b.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		b.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "BENCH"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		b.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		b.Fatal(err)
	}
	return &DigitalCertificate{PrivateKey: key, Certificate: leaf}
}

func BenchmarkBuildDE(b *testing.B) {
	in := testInput()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in.NumeroDoc = 1000 + (i % 9000)
		in.CodigoSeguridad = 0 // regenerar CDC cada iteración
		if _, err := BuildDE(in); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFirmarYSerializar(b *testing.B) {
	cert := benchCert(b)
	in := testInput()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		in.NumeroDoc = 1000 + (i % 9000)
		in.CodigoSeguridad = 0
		rde, err := BuildDE(in)
		if err != nil {
			b.Fatal(err)
		}
		cdc := rde.DE.Id
		fe := rde.DE.GDatGralOpe.DFeEmiDE
		tot := rde.DE.GTotSub
		b.StartTimer()

		_, _, err = FirmarYSerializar(rde, cert, func(dv string) (string, error) {
			return BuildCarQR(QRParams{
				CDC: cdc, FeEmiDE: fe, RecID: "1234567", RecField: "dNumIDRec",
				TotGralOpe: DecimalPlain(tot.DTotGralOpe), TotIVA: DecimalPlain(tot.DTotIVA),
				Items: 1, DigestValue: dv, IdCSC: "0001",
				CSC: "ABCD0000000000000000000000000000", QRBase: QRBaseURLTest,
			})
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildDEAndFirmar(b *testing.B) {
	cert := benchCert(b)
	in := testInput()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in.NumeroDoc = 1000 + (i % 9000)
		in.CodigoSeguridad = 0
		rde, err := BuildDE(in)
		if err != nil {
			b.Fatal(err)
		}
		cdc := rde.DE.Id
		fe := rde.DE.GDatGralOpe.DFeEmiDE
		tot := rde.DE.GTotSub
		_, _, err = FirmarYSerializar(rde, cert, func(dv string) (string, error) {
			return BuildCarQR(QRParams{
				CDC: cdc, FeEmiDE: fe, RecID: "1234567", RecField: "dNumIDRec",
				TotGralOpe: DecimalPlain(tot.DTotGralOpe), TotIVA: DecimalPlain(tot.DTotIVA),
				Items: 1, DigestValue: dv, IdCSC: "0001",
				CSC: "ABCD0000000000000000000000000000", QRBase: QRBaseURLTest,
			})
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}
