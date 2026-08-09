package kude

import (
	"strings"
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

func TestBuildAndRenderKuDE(t *testing.T) {
	rde := buildTestRDE(t)
	qr := "https://ekuatia.set.gov.py/consultas-test/qr?nVersion=150&Id=" + rde.DE.Id

	for _, tmpl := range []string{TemplateMinimalista, TemplateCorporativa} {
		data, err := BuildKudeData(rde, qr, "test", Branding{
			TemplateID:    tmpl,
			ColorPrimario: "#0f172a",
			NotasFooter:   "Gracias por su compra",
		})
		if err != nil {
			t.Fatalf("BuildKudeData(%s): %v", tmpl, err)
		}
		html, err := RenderKuDEHTML(data)
		if err != nil {
			t.Fatalf("RenderKuDEHTML(%s): %v", tmpl, err)
		}
		if !strings.Contains(html, "<html") {
			t.Fatalf("salida de %s no parece HTML: %q", tmpl, html[:min(80, len(html))])
		}
		if !strings.Contains(html, data.Emisor.Nombre) {
			t.Fatalf("HTML de %s no contiene el nombre del emisor", tmpl)
		}
		if data.MostrarLeyendaPrueba && !strings.Contains(html, data.LeyendaPrueba) {
			t.Fatalf("HTML de %s no muestra la leyenda de ambiente de prueba", tmpl)
		}
	}
}

func TestBuildKudeData_RequiereQR(t *testing.T) {
	rde := buildTestRDE(t)
	if _, err := BuildKudeData(rde, "", "test", Branding{}); err == nil {
		t.Fatal("esperaba error por qrURL vacío")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
