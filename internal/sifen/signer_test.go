package sifen

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/shopspring/decimal"
)

func TestFirmarYSerializar_SelfVerify(t *testing.T) {
	cert := mustTestCert(t)
	cdc := "01060389648001001000012812026071415704692203"
	rde := NewRDE(cdc)
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	fe := now.Format("2006-01-02T15:04:05")
	rde.DE.DFecFirma = fe
	rde.DE.DSisFact = 1
	rde.DE.GOpeDE = GOpeDE{ITipEmi: 1, DDesTipEmi: "Normal", DCodSeg: "123456789"}
	rde.DE.GTimb = GTimb{
		ITiDE: 1, DDesTiDE: "Factura electrónica",
		DNumTim: "06038964", DEst: "001", DPunExp: "001", DNumDoc: "0000128", DFeIniT: "2026-06-02",
	}
	rec, err := NewReceptorCI("1234567", "CLIENTE DE PRUEBA", ReceptorGeo{Dir: "X", NumCas: "1", CDep: 11, DDesDep: "CENTRAL"})
	if err != nil {
		t.Fatal(err)
	}
	rde.DE.GDatGralOpe = GDatGralOpe{
		DFeEmiDE: fe,
		GOpeCom:  &GOpeCom{ITipTra: 1, DDesTipTra: "Venta de mercadería", ITImp: 1, DDesTImp: "IVA", CMoneOpe: "PYG", DDesMoneOpe: "Guarani"},
		GEmis: GEmis{
			DRucEm: "6038964", DDVEmi: 8, ITipCont: 1, CTipReg: 8,
			DNomEmi: "DE generado en ambiente de prueba - sin valor comercial ni fiscal",
			DDirEmi: "CALLE", DNumCas: "0",
			CDepEmi: 11, DDesDepEmi: "CENTRAL", CCiuEmi: 5598, DDesCiuEmi: "CAPIATA",
			GActEco: []GActEco{{CActEco: "47411", DDesActEco: "COMERCIO"}},
		},
		GDatRec: rec,
	}
	total := decimal.NewFromInt(110000)
	cond, err := CondicionCreditoPlazo("28", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rde.DE.GDtipDE = GDtipDE{
		GCamFE:   &GCamFE{IIndPres: 1, DDesIndPres: "Operación presencial"},
		GCamCond: cond,
	}
	if err := rde.DE.AgregarItem(ItemInput{
		Codigo:         "A",
		Descripcion:    "DOCUMENTO ELECTRÓNICO SIN VALOR COMERCIAL NI FISCAL - GENERADO EN AMBIENTE DE PRUEBA",
		Cantidad:       decimal.NewFromInt(1),
		PrecioUnitario: total,
		AfectacionIVA:  AfecIVAGravado,
		TasaIVA:        10,
	}, "PYG"); err != nil {
		t.Fatal(err)
	}
	rde.DE.GTotSub = &GTotSub{
		DSub10: total, DTotOpe: total, DTotGralOpe: total, DTotIVA10: decimal.NewFromInt(10000),
		DTotIVA: decimal.NewFromInt(10000), DBaseGrav10: decimal.NewFromInt(100000), DTBasGraIVA: decimal.NewFromInt(100000),
	}

	dv, xmlOut, err := FirmarYSerializar(rde, cert, func(digest string) (string, error) {
		return BuildCarQR(QRParams{
			CDC: cdc, FeEmiDE: fe, RecID: "1234567",
			TotGralOpe: "110000", TotIVA: "10000", Items: 1,
			DigestValue: digest, IdCSC: "0001", CSC: "ABCD0000000000000000000000000000",
			QRBase: QRBaseURLTest,
		})
	})
	if err != nil {
		t.Fatalf("FirmarYSerializar: %v", err)
	}
	if dv == "" || len(xmlOut) < 100 {
		t.Fatal("salida vacía")
	}
	if err := VerifySignedXML(xmlOut, cert); err != nil {
		t.Fatalf("VerifySignedXML: %v", err)
	}
	s := string(xmlOut)
	for _, part := range []string{"SignatureValue", "SignedInfo", "gCamFuFD", "DigestValue"} {
		if !strings.Contains(s, part) {
			t.Fatalf("XML incompleto, falta %s", part)
		}
	}

	// Exc-C14N debe incluir xmlns default (SIFEN PKI lo exige; goxmldsig no hereda del padre).
	de := mustFind(t, xmlOut, "./DE")
	deC14N, err := exclusiveC14NWithDefaultNS(de, SifenNS)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(deC14N), `xmlns="`+SifenNS+`"`) {
		t.Fatalf("C14N DE sin xmlns sifen: %s", trunc(deC14N, 120))
	}
	si := mustFind(t, xmlOut, "./Signature/SignedInfo")
	siC14N, err := exclusiveC14NWithDefaultNS(si, DSigNS)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(siC14N), `xmlns="`+DSigNS+`"`) {
		t.Fatalf("C14N SignedInfo sin xmlns dsig: %s", trunc(siC14N, 120))
	}
}

func mustFind(t *testing.T, xmlOut []byte, path string) *etree.Element {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(stripXMLHeader(xmlOut)); err != nil {
		t.Fatal(err)
	}
	el := doc.Root().FindElement(path)
	if el == nil {
		t.Fatalf("no encontrado %s", path)
	}
	return el
}

func trunc(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n])
}

func mustTestCert(t *testing.T) *DigitalCertificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "TEST"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &DigitalCertificate{PrivateKey: key, Certificate: leaf}
}
