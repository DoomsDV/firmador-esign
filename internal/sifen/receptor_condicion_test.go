package sifen

import (
	"encoding/xml"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

func TestNewReceptorCI_Marshaling(t *testing.T) {
	rec, err := NewReceptorCI("1234567", "CLIENTE DE PRUEBA", ReceptorGeo{
		Dir: "CALLE 1", NumCas: "10",
		CDep: 11, DDesDep: "CENTRAL",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.INatRec != 2 || rec.ITiOpe != 2 {
		t.Fatalf("naturaleza/tipo: got iNatRec=%d iTiOpe=%d", rec.INatRec, rec.ITiOpe)
	}

	b, err := xml.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "<iNatRec>2</iNatRec>") {
		t.Fatalf("falta iNatRec=2: %s", s)
	}
	if !strings.Contains(s, "<dNumIDRec>1234567</dNumIDRec>") {
		t.Fatalf("falta dNumIDRec: %s", s)
	}
	for _, forbidden := range []string{"dRucRec", "dDVRec", "iTiContRec"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("CI no debe emitir %s: %s", forbidden, s)
		}
	}
}

func TestNewReceptorRUC_Marshaling(t *testing.T) {
	rec, err := NewReceptorRUC("80012345", 6, 2, "EMPRESA SA", 1, ReceptorGeo{})
	if err != nil {
		t.Fatal(err)
	}

	b, err := xml.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "<iNatRec>1</iNatRec>") || !strings.Contains(s, "<dRucRec>80012345</dRucRec>") {
		t.Fatalf("RUC incompleto: %s", s)
	}
	for _, forbidden := range []string{"iTipIDRec", "dNumIDRec", "dDTipIDRec"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("RUC no debe emitir %s: %s", forbidden, s)
		}
	}
}

func TestValidateReceptor_RejectsIllegalCombos(t *testing.T) {
	_, err := NewReceptorCI("1", "AB", ReceptorGeo{})
	if err == nil {
		t.Fatal("esperado error por nombre corto")
	}

	bad := GDatRec{INatRec: 1, ITiOpe: 2, ITipIDRec: 1, DNumIDRec: "123", DNomRec: "TEST"}
	if err := ValidateReceptor(bad); err == nil {
		t.Fatal("contribuyente con CI debería fallar")
	}

	bad2 := GDatRec{INatRec: 2, ITiOpe: 2, DRucRec: "1", DNomRec: "CLIENTE DE PRUEBA", ITipIDRec: 1, DNumIDRec: "1"}
	if err := ValidateReceptor(bad2); err == nil {
		t.Fatal("no contribuyente con RUC debería fallar")
	}
}

func TestCondicionCreditoPlazo_Marshaling(t *testing.T) {
	cond, err := CondicionCreditoPlazo("28", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := xml.Marshal(cond)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "<iCondOpe>2</iCondOpe>") || !strings.Contains(s, "<gPagCred>") {
		t.Fatalf("falta crédito/gPagCred: %s", s)
	}
	if !strings.Contains(s, "<dPlazoCre>28</dPlazoCre>") {
		t.Fatalf("falta dPlazoCre: %s", s)
	}
	if strings.Contains(s, "gPaConEIni") {
		t.Fatalf("crédito puro no debe tener gPaConEIni: %s", s)
	}
}

func TestCondicionContado_Marshaling(t *testing.T) {
	cond, err := CondicionContado(PagoEfectivoPYG(decimal.NewFromInt(1000)))
	if err != nil {
		t.Fatal(err)
	}
	b, err := xml.Marshal(cond)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "<iCondOpe>1</iCondOpe>") || !strings.Contains(s, "gPaConEIni") {
		t.Fatalf("contado incompleto: %s", s)
	}
	if strings.Contains(s, "gPagCred") {
		t.Fatalf("contado no debe tener gPagCred: %s", s)
	}
}

func TestCondicionCreditoCuotas_Marshaling(t *testing.T) {
	cuotas := []GCuota{{
		CMoneCuo: "PYG", DDMoneCuo: "Guarani",
		DMonCuota: decimal.NewFromInt(50000), DVencCuo: "2026-08-01",
	}}
	cond, err := CondicionCreditoCuotas(1, cuotas, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := xml.Marshal(cond)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "<dCuotas>1</dCuotas>") || !strings.Contains(s, "gCuotas") {
		t.Fatalf("cuotas incompletas: %s", s)
	}
}
