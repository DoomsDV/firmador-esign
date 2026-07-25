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
