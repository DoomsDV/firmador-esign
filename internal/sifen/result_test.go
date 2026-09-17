package sifen

import "testing"

func TestParseConsultaResultDetectsCancellation(t *testing.T) {
	body := []byte(`<rResEnviConsDe><dCodRes>0422</dCodRes><xContenDE><rContDe><xContEv><rContEv><xEvento><rGeVeCan/></xEvento></rContEv></xContEv></rContDe></xContenDE></rResEnviConsDe>`)
	got := ParseConsultaResult(body)
	if !got.Found || !got.Cancelado {
		t.Fatalf("consulta = %+v, esperaba CDC encontrado y cancelado", got)
	}
}

func TestEstadoDEResultKeepsAmbiguousDocumentSigned(t *testing.T) {
	if got, done := EstadoDEResult(""); got != "FIRMADO" || done {
		t.Fatalf("estado vacío = %q, %v", got, done)
	}
	if got, done := EstadoDEResult("0260"); got != "APROBADO" || !done {
		t.Fatalf("0260 = %q, %v", got, done)
	}
}
