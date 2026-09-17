package sifen

import (
	"strings"
	"testing"
)

func TestBuildSiRecepDEEnvelope_CompactSOAP(t *testing.T) {
	rde := []byte(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<rDE xmlns="http://ekuatia.set.gov.py/sifen/xsd"><dVerFor>150</dVerFor></rDE>`)
	got, err := buildSiRecepDEEnvelope(42, rde)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)

	wantParts := []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope">`,
		`<soap:Header></soap:Header>`,
		`<soap:Body>`,
		`<rEnviDe xmlns="http://ekuatia.set.gov.py/sifen/xsd">`,
		`<dId>42</dId>`,
		`<xDE><rDE xmlns="http://ekuatia.set.gov.py/sifen/xsd"><dVerFor>150</dVerFor></rDE></xDE>`,
		`</rEnviDe></soap:Body></soap:Envelope>`,
	}
	for _, p := range wantParts {
		if !strings.Contains(s, p) {
			t.Fatalf("envelope incompleto, falta %q\n%s", p, s)
		}
	}
	// Sin saltos ni espacios entre cierre de Header y Body.
	if strings.Contains(s, ">\n") || strings.Contains(s, "> <") {
		t.Fatalf("envelope tiene whitespace entre tags:\n%s", s)
	}
	if strings.Contains(s, "SOAPAction") {
		t.Fatal("el body no debe mencionar SOAPAction")
	}
}

func TestBuildSiConsDEEnvelope(t *testing.T) {
	cdc := "01060389648001001000012812026071415704692203"
	got, err := buildSiConsDEEnvelope(99, cdc)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	for _, part := range []string{"<rEnviConsDe", "<dId>99</dId>", "<dCDC>" + cdc + "</dCDC>"} {
		if !strings.Contains(s, part) {
			t.Fatalf("envelope de consulta incompleto, falta %q: %s", part, s)
		}
	}
	if _, err := buildSiConsDEEnvelope(1, "abc"); err == nil {
		t.Fatal("esperaba rechazo por CDC inválido")
	}
}
