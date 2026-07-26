package sifen

import "testing"

func TestAssertSafeTestURL_AllowsTest(t *testing.T) {
	ok := []string{
		"https://sifen-test.set.gov.py/de/ws/sync/recibe.wsdl",
		"https://sifen-test.set.gov.py/de/ws/async/recibe-lote.wsdl",
		"https://ekuatia.set.gov.py/consultas-test/qr?",
		"https://www.ekuatia.set.gov.py/consultas-test/qr?",
	}
	for _, u := range ok {
		if err := AssertSafeTestURL(u); err != nil {
			t.Fatalf("debería permitir %q: %v", u, err)
		}
	}
}

func TestAssertSafeTestURL_DeniesProd(t *testing.T) {
	bad := []string{
		"https://sifen.set.gov.py/de/ws/sync/recibe.wsdl",
		"https://sifen.set.gov.py/de/ws/async/recibe-lote.wsdl",
		"https://ekuatia.set.gov.py/consultas/qr?",
		"https://www.ekuatia.set.gov.py/consultas/qr?nVersion=150",
		"https://google.com/",
		"",
	}
	for _, u := range bad {
		if err := AssertSafeTestURL(u); err == nil {
			t.Fatalf("debería rechazar producción/host inválido: %q", u)
		}
	}
}

func TestAssertSafeURL_ProdAllowsProd(t *testing.T) {
	ok := []string{
		"https://sifen.set.gov.py/de/ws/sync/recibe.wsdl",
		"https://ekuatia.set.gov.py/consultas/qr?",
	}
	for _, u := range ok {
		if err := AssertSafeURL(u, EnvProd); err != nil {
			t.Fatalf("prod debería permitir %q: %v", u, err)
		}
	}
}

func TestAssertSafeURL_RejectsMixing(t *testing.T) {
	// key/ambiente prod jamás pega a hosts de prueba y viceversa.
	if err := AssertSafeURL("https://sifen-test.set.gov.py/de/ws/sync/recibe.wsdl", EnvProd); err == nil {
		t.Fatal("prod debe rechazar host de prueba (mezcla)")
	}
	if err := AssertSafeURL("https://ekuatia.set.gov.py/consultas-test/qr?", EnvProd); err == nil {
		t.Fatal("prod debe rechazar QR consultas-test (mezcla)")
	}
	if err := AssertSafeURL("https://sifen.set.gov.py/de/ws/sync/recibe.wsdl", EnvTest); err == nil {
		t.Fatal("test debe rechazar host de producción (mezcla)")
	}
}

func TestBuildCarQR_RejectsProdBase(t *testing.T) {
	_, err := BuildCarQR(QRParams{
		CDC:         "01444444017001001001452822017012515873260988",
		FeEmiDE:     "2017-01-25T09:35:17",
		RecID:       "88899990",
		TotGralOpe:  "300000",
		TotIVA:      "27272",
		Items:       2,
		DigestValue: "yzGYhUx1/XYYzksWB+fPR3Qc50c=",
		IdCSC:       "0001",
		CSC:         "ABCD0000000000000000000000000000",
		QRBase:      "https://ekuatia.set.gov.py/consultas/qr?",
	})
	if err == nil {
		t.Fatal("BuildCarQR debe rechazar base de producción")
	}
}
