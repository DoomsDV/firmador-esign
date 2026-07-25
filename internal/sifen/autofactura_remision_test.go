package sifen

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestBuildDE_Autofactura(t *testing.T) {
	in := testInput()
	in.TipoDE = 4
	ubic := UbicacionAE{
		Direccion: "CALLE VENDEDOR", NumeroCasa: "0",
		Departamento: 12, DesDepartamento: "CENTRAL",
		Ciudad: 3568, DesCiudad: "CAPIATA",
	}
	camAE, err := NewCamAE(1, TipIDCedulaPY, "3456789", "VENDEDOR NO CONTRIBUYENTE", ubic, ubic)
	if err != nil {
		t.Fatal(err)
	}
	in.CamAE = camAE
	// La autofactura exige documento asociado (constancia de no ser contribuyente).
	asoc, err := NewDEAsocConstancia(1, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	in.DocsAsociados = []GCamDEAsoc{asoc}

	rde, err := BuildDE(in)
	if err != nil {
		t.Fatalf("BuildDE autofactura: %v", err)
	}
	if rde.DE.GTimb.DDesTiDE != "Autofactura electrónica" {
		t.Errorf("dDesTiDE: %q", rde.DE.GTimb.DDesTiDE)
	}
	if rde.DE.GDtipDE.GCamAE == nil || rde.DE.GDtipDE.GCamAE.INatVen != 1 {
		t.Fatalf("gCamAE incorrecto: %+v", rde.DE.GDtipDE.GCamAE)
	}
	// Autofactura: los ítems no llevan gCamIVA (validación 1901) pero sí gValorItem.
	for _, it := range rde.DE.GDtipDE.GCamItem {
		if it.GCamIVA != nil {
			t.Error("autofactura no debe llevar gCamIVA por ítem")
		}
		if it.GValorItem == nil {
			t.Error("autofactura sí debe llevar gValorItem por ítem")
		}
	}
	if rde.DE.GTotSub == nil {
		t.Fatal("autofactura debe informar gTotSub")
	}

	b, _ := xml.Marshal(rde)
	s := string(b)
	// gCamAE debe ir antes de gCamItem (orden XSD).
	if strings.Index(s, "<gCamAE>") > strings.Index(s, "<gCamItem>") {
		t.Error("gCamAE debe preceder a gCamItem")
	}
	if !strings.Contains(s, "<dNomVen>VENDEDOR NO CONTRIBUYENTE</dNomVen>") {
		t.Error("falta dNomVen")
	}
	if !strings.Contains(s, "<gCamDEAsoc>") || !strings.Contains(s, "<iTipCons>1</iTipCons>") {
		t.Error("falta constancia en gCamDEAsoc")
	}
	if strings.Contains(s, "<gCamIVA>") {
		t.Error("autofactura no debe contener gCamIVA")
	}
}

func TestBuildDE_Remision(t *testing.T) {
	in := testInput()
	in.TipoDE = 7
	in.Condicion = nil // remisión no lleva gCamCond

	nre, err := NewCamNRE(1, 1, 25) // traslado por venta, emisor responsable, 25 km
	if err != nil {
		t.Fatal(err)
	}
	in.CamNRE = nre
	in.Transporte = &GTransp{
		IModTrans: 1, DDesModTrans: "Terrestre", IRespFlete: 1,
		DIniTras: "2026-07-22",
		GCamSal: &GCamSal{
			DDirLocSal: "DEPOSITO CENTRAL", DNumCasSal: "0",
			CDepSal: 12, DDesDepSal: "CENTRAL", CCiuSal: 3568, DDesCiuSal: "CAPIATA",
		},
		GCamEnt: []GCamEnt{{
			DDirLocEnt: "CLIENTE FINAL", DNumCasEnt: "0",
			CDepEnt: 12, DDesDepEnt: "CENTRAL", CCiuEnt: 3568, DDesCiuEnt: "CAPIATA",
		}},
		GVehTras: []GVehTras{{
			DTiVehTras: "CAMION", DMarVeh: "FORD", DTipIdenVeh: 2, DNroMatVeh: "ABC123",
		}},
		GCamTrans: &GCamTrans{
			INatTrans: 1, DNomTrans: "TRANSPORTISTA SA", DRucTrans: "80012345", DDVTrans: 6,
			DNumIDChof: "1234567", DNomChof: "JUAN PEREZ",
			DDomFisc: "AV SIEMPRE VIVA 123", DDirChof: "AV SIEMPRE VIVA 123",
		},
	}

	rde, err := BuildDE(in)
	if err != nil {
		t.Fatalf("BuildDE remisión: %v", err)
	}
	if rde.DE.GTimb.DDesTiDE != "Nota de remisión electrónica" {
		t.Errorf("dDesTiDE: %q", rde.DE.GTimb.DDesTiDE)
	}
	// Remisión (C002=7): sin gTotSub, sin gOpeCom, y sus ítems sin valor ni IVA.
	if rde.DE.GTotSub != nil {
		t.Error("remisión no debe informar gTotSub")
	}
	if rde.DE.GDatGralOpe.GOpeCom != nil {
		t.Error("remisión no debe informar gOpeCom")
	}
	for _, it := range rde.DE.GDtipDE.GCamItem {
		if it.GValorItem != nil || it.GCamIVA != nil {
			t.Error("ítems de remisión no llevan gValorItem ni gCamIVA")
		}
	}

	b, _ := xml.Marshal(rde)
	s := string(b)
	// gTransp debe ir después de gCamItem (orden XSD).
	if strings.Index(s, "<gCamItem>") > strings.Index(s, "<gTransp>") {
		t.Error("gTransp debe ir después de gCamItem")
	}
	if strings.Contains(s, "<gCamCond>") {
		t.Error("remisión no debe llevar gCamCond")
	}
	if strings.Contains(s, "<gOpeCom>") || strings.Contains(s, "<gTotSub>") {
		t.Error("remisión no debe contener gOpeCom ni gTotSub")
	}
	for _, want := range []string{"<gCamNRE>", "<gCamSal>", "<gCamEnt>", "<gVehTras>", "<gCamTrans>", "<dKmR>25</dKmR>"} {
		if !strings.Contains(s, want) {
			t.Errorf("falta %s en remisión", want)
		}
	}
}

func TestBuildDE_RemisionSinTransporte(t *testing.T) {
	in := testInput()
	in.TipoDE = 7
	in.Condicion = nil
	nre, _ := NewCamNRE(1, 1, 25)
	in.CamNRE = nre
	if _, err := BuildDE(in); err == nil {
		t.Fatal("esperaba error por falta de gTransp")
	}
}

func TestNewCamNRE_Validaciones(t *testing.T) {
	if _, err := NewCamNRE(99, 1, 100000); err == nil {
		t.Error("esperaba error por dKmR fuera de rango")
	}
	if _, err := NewCamNRE(50, 1, 10); err == nil {
		t.Error("esperaba error por motivo inválido")
	}
}
