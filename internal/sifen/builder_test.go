package sifen

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func testEmisor() Emisor {
	return Emisor{
		RUC: "6038964", DV: 8, TipoContribuyente: 1, TipoRegimen: 8,
		Nombre: "EMISOR DE PRUEBA", Direccion: "CALLE 1", NumeroCasa: "0",
		Departamento: 12, DesDepartamento: "CENTRAL",
		Distrito: 153, DesDistrito: "CAPIATA",
		Ciudad: 3568, DesCiudad: "CAPIATA",
		Actividades: []GActEco{{CActEco: "74909", DDesActEco: "OTRAS"}},
	}
}

func testInput() DocumentInput {
	rec, _ := NewReceptorCI("1234567", "CLIENTE DE PRUEBA", ReceptorGeo{CDep: 12, DDesDep: "CENTRAL"})
	cond, _ := CondicionCreditoPlazo("28", nil, nil)
	return DocumentInput{
		TipoDE: 1, Establecimiento: "001", PuntoExp: "001", NumeroDoc: 123,
		NumTimbrado: "06038964", FeIniT: "2026-07-09", TipoEmision: 1,
		FechaFirma: time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC),
		Emisor:     testEmisor(), Receptor: rec,
		TipoTransaccion: 1, DesTipoTransaccion: "Venta de mercadería",
		Moneda: "PYG", DesMoneda: "Guarani",
		Items: []ItemInput{{
			Codigo: "A", Descripcion: "X", Cantidad: di(1), PrecioUnitario: di(150000),
			AfectacionIVA: AfecIVAGravado, TasaIVA: 10,
		}},
		Condicion: cond,
		IndPres:   1, DesIndPres: "Operación presencial",
	}
}

func TestBuildDE_FE(t *testing.T) {
	rde, err := BuildDE(testInput())
	if err != nil {
		t.Fatalf("BuildDE: %v", err)
	}
	if len(rde.DE.Id) != 44 {
		t.Fatalf("CDC inválido: %q", rde.DE.Id)
	}
	if rde.DE.GTimb.ITiDE != 1 || rde.DE.GTimb.DDesTiDE != "Factura electrónica" {
		t.Errorf("gTimb: %+v", rde.DE.GTimb)
	}
	if rde.DE.GDtipDE.GCamFE == nil {
		t.Error("falta gCamFE en FE")
	}
	assertDec(t, "dTotGralOpe", rde.DE.GTotSub.DTotGralOpe, 150000)

	b, err := xml.Marshal(rde)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "<dTotalGs>") {
		t.Error("PYG no debe emitir dTotalGs")
	}
}

func TestBuildDE_Multimoneda(t *testing.T) {
	in := testInput()
	in.Moneda = "USD"
	in.DesMoneda = "Dolar"
	in.CondicionTipoCambio = 1
	tc := decimal.NewFromInt(7300)
	in.TipoCambio = &tc
	in.Items = []ItemInput{{
		Codigo: "A", Descripcion: "X", Cantidad: di(1), PrecioUnitario: decimal.RequireFromString("20.55"),
		AfectacionIVA: AfecIVAGravado, TasaIVA: 10,
	}}

	rde, err := BuildDE(in)
	if err != nil {
		t.Fatalf("BuildDE USD: %v", err)
	}
	if rde.DE.GTotSub.DTotalGs == nil {
		t.Fatal("USD debe informar dTotalGs")
	}
	// 20.55 * 7300 = 150015
	assertDec(t, "dTotalGs", *rde.DE.GTotSub.DTotalGs, 150015)
	if rde.DE.GDatGralOpe.GOpeCom.DTiCam == nil || rde.DE.GDatGralOpe.GOpeCom.DCondTiCam != 1 {
		t.Errorf("gOpeCom multimoneda incompleto: %+v", rde.DE.GDatGralOpe.GOpeCom)
	}

	b, _ := xml.Marshal(rde)
	if !strings.Contains(string(b), "<dTotalGs>") {
		t.Error("USD debe emitir dTotalGs en el XML")
	}
}

func TestBuildDE_Multimoneda_SinTipoCambio(t *testing.T) {
	in := testInput()
	in.Moneda = "USD"
	in.CondicionTipoCambio = 1
	if _, err := BuildDE(in); err == nil {
		t.Fatal("esperaba error por falta de dTiCam")
	}
}

func TestBuildDE_NCE(t *testing.T) {
	in := testInput()
	in.TipoDE = 5
	in.Condicion = nil
	in.MotivoEmision = 2
	asoc, err := NewDEAsocElectronico("01060389648001001000012812026071415704692203")
	if err != nil {
		t.Fatal(err)
	}
	in.DocsAsociados = []GCamDEAsoc{asoc}

	rde, err := BuildDE(in)
	if err != nil {
		t.Fatalf("BuildDE NCE: %v", err)
	}
	if rde.DE.GTimb.ITiDE != 5 || rde.DE.GTimb.DDesTiDE != "Nota de crédito electrónica" {
		t.Errorf("gTimb NCE: %+v", rde.DE.GTimb)
	}
	if rde.DE.GDtipDE.GCamNCDE == nil || rde.DE.GDtipDE.GCamNCDE.IMotEmi != 2 {
		t.Errorf("falta/incorrecto gCamNCDE: %+v", rde.DE.GDtipDE.GCamNCDE)
	}
	if len(rde.DE.GCamDEAsoc) != 1 || rde.DE.GCamDEAsoc[0].DCdCDERef == "" {
		t.Errorf("falta gCamDEAsoc: %+v", rde.DE.GCamDEAsoc)
	}
	// gCamDEAsoc debe ir después de gTotSub (hijo directo de DE).
	b, _ := xml.Marshal(rde)
	s := string(b)
	if strings.Index(s, "<gTotSub>") > strings.Index(s, "<gCamDEAsoc>") {
		t.Error("gCamDEAsoc debe ir después de gTotSub")
	}
}

func TestBuildDE_NCE_SinDocAsociado(t *testing.T) {
	in := testInput()
	in.TipoDE = 5
	in.Condicion = nil
	in.MotivoEmision = 2
	if _, err := BuildDE(in); err == nil {
		t.Fatal("esperaba error por falta de documento asociado")
	}
}
