package kude

import (
	"bytes"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/DoomsDV/firmador-e/internal/sifen"
)

func buildTestRDE(t *testing.T) *sifen.RDE {
	t.Helper()
	rec, err := sifen.NewReceptorCI("1234567", "CLIENTE DE PRUEBA", sifen.ReceptorGeo{CDep: 12, DDesDep: "CENTRAL"})
	if err != nil {
		t.Fatal(err)
	}
	cond, err := sifen.CondicionCreditoPlazo("28", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rde, err := sifen.BuildDE(sifen.DocumentInput{
		TipoDE: 1, Establecimiento: "001", PuntoExp: "001", NumeroDoc: 123,
		NumTimbrado: "06038964", FeIniT: "2026-07-09", TipoEmision: 1,
		FechaFirma: time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC),
		Emisor: sifen.Emisor{
			RUC: "6038964", DV: 8, TipoContribuyente: 1, TipoRegimen: 8,
			Nombre: "EMISOR DE PRUEBA", Direccion: "ROJAS CAÑADA", NumeroCasa: "0",
			Departamento: 12, DesDepartamento: "CENTRAL",
			Ciudad: 3568, DesCiudad: "CAPIATA",
			Actividades: []sifen.GActEco{{CActEco: "74909", DDesActEco: "OTRAS ACTIVIDADES"}},
		},
		Receptor:        rec,
		TipoTransaccion: 1, DesTipoTransaccion: "Venta de mercadería",
		Moneda: "PYG", DesMoneda: "Guarani",
		Items: []sifen.ItemInput{{
			Codigo: "A", Descripcion: "SERVICIO DE PRUEBA CON TILDES ÁÉÍ Ñ",
			Cantidad: decimal.NewFromInt(1), PrecioUnitario: decimal.NewFromInt(150000),
			AfectacionIVA: sifen.AfecIVAGravado, TasaIVA: 10,
		}},
		Condicion: cond, IndPres: 1, DesIndPres: "Operación presencial",
	})
	if err != nil {
		t.Fatal(err)
	}
	return rde
}

func TestRenderKuDE(t *testing.T) {
	rde := buildTestRDE(t)
	qr := "https://ekuatia.set.gov.py/consultas-test/qr?nVersion=150&Id=" + rde.DE.Id

	pdf, err := RenderKuDE(rde, qr)
	if err != nil {
		t.Fatalf("RenderKuDE: %v", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("salida no es PDF: %q", pdf[:min(8, len(pdf))])
	}
	if len(pdf) < 1000 {
		t.Fatalf("PDF sospechosamente pequeño: %d bytes", len(pdf))
	}
}

func TestRenderKuDE_RequiereQR(t *testing.T) {
	rde := buildTestRDE(t)
	if _, err := RenderKuDE(rde, ""); err == nil {
		t.Fatal("esperaba error por qrURL vacío")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
