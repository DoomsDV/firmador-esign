package sifen

import (
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
)

const testCDC = "01060389648001001000012812026071415704692203"

func TestNewEventoCancelacion_Validaciones(t *testing.T) {
	fecha := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	if _, err := NewEventoCancelacion(1, "123", "motivo válido", fecha); err == nil {
		t.Error("esperaba error por CDC inválido")
	}
	if _, err := NewEventoCancelacion(1, testCDC, "abc", fecha); err == nil {
		t.Error("esperaba error por motivo corto")
	}
	ev, err := NewEventoCancelacion(1, testCDC, "Documento emitido con datos incorrectos", fecha)
	if err != nil {
		t.Fatal(err)
	}
	if ev.GGroupTiEvt.RGeVeCan == nil {
		t.Fatalf("evento cancelación mal armado: %+v", ev)
	}
	if ev.GGroupTiEvt.RGeVeCan.Id != testCDC {
		t.Errorf("CDC del evento: got %s", ev.GGroupTiEvt.RGeVeCan.Id)
	}
}

func TestNewEventoInutilizacion_Rango(t *testing.T) {
	fecha := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	if _, err := NewEventoInutilizacion(1, "06038964", "001", "001", 10, 2000, 1, "motivo del rango", fecha); err == nil {
		t.Error("esperaba error por rango > 1000")
	}
	ev, err := NewEventoInutilizacion(1, "06038964", "001", "001", 10, 20, 1, "rango anulado por error", fecha)
	if err != nil {
		t.Fatal(err)
	}
	inu := ev.GGroupTiEvt.RGeVeInu
	if inu == nil || inu.DNumIn != "0000010" || inu.DNumFin != "0000020" {
		t.Fatalf("inutilización mal armada: %+v", inu)
	}
}

func TestFirmarEvento_Estructura(t *testing.T) {
	cert := mustTestCert(t)
	fecha := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	ev, err := NewEventoCancelacion(1, testCDC, "Documento emitido con datos incorrectos", fecha)
	if err != nil {
		t.Fatal(err)
	}
	xmlOut, err := FirmarEvento(ev, cert)
	if err != nil {
		t.Fatalf("FirmarEvento: %v", err)
	}
	s := string(xmlOut)
	for _, part := range []string{"<gGroupGesEve", "<rGesEve>", "<rEve Id=\"1\">", "<rGeVeCan>", "<Signature", "SignatureValue", "DigestValue"} {
		if !strings.Contains(s, part) {
			t.Fatalf("XML de evento incompleto, falta %q\n%s", part, s)
		}
	}

	// La firma debe referenciar el Id del rEve y verificar el digest sobre rEve
	// con xmlns sifen inyectado.
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(xmlOut); err != nil {
		t.Fatal(err)
	}
	reve := doc.Root().FindElement("./rGesEve/rEve")
	sig := doc.Root().FindElement("./rGesEve/Signature")
	if reve == nil || sig == nil {
		t.Fatal("faltan rEve o Signature")
	}
	uri := sig.FindElement("./SignedInfo/Reference").SelectAttrValue("URI", "")
	if uri != "#1" {
		t.Errorf("URI de referencia: got %q want #1", uri)
	}
}
