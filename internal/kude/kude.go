// Package kude genera la representación gráfica (KuDE) de un documento
// electrónico en formato PDF, con el QR embebido, según el Manual Técnico.
package kude

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"
	"github.com/shopspring/decimal"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/DoomsDV/firmador-e/internal/sifen"
)

// leyendaPrueba es la advertencia obligatoria en ambiente de homologación.
const leyendaPrueba = "DOCUMENTO GENERADO EN AMBIENTE DE PRUEBA - SIN VALOR COMERCIAL NI FISCAL"

// RenderKuDE construye el PDF del KuDE a partir del rDE firmado y la URL del QR
// (dCarQR). El resultado son los bytes del PDF listos para escribir o servir.
func RenderKuDE(rde *sifen.RDE, qrURL string) ([]byte, error) {
	if rde == nil || rde.DE == nil {
		return nil, fmt.Errorf("rDE/DE nulo")
	}
	if qrURL == "" {
		return nil, fmt.Errorf("qrURL vacío: el KuDE requiere el dCarQR")
	}

	de := rde.DE
	emis := de.GDatGralOpe.GEmis
	timb := de.GTimb
	rec := de.GDatGralOpe.GDatRec
	tot := de.GTotSub
	moneda := "PYG"
	if de.GDatGralOpe.GOpeCom != nil && de.GDatGralOpe.GOpeCom.CMoneOpe != "" {
		moneda = de.GDatGralOpe.GOpeCom.CMoneOpe
	}

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetTitle("KuDE "+de.Id, true)
	pdf.SetMargins(12, 12, 12)
	pdf.SetAutoPageBreak(true, 12)
	pdf.AddPage()
	tr := pdf.UnicodeTranslatorFromDescriptor("") // UTF-8 -> cp1252

	writeHeader(pdf, tr, emis, timb)
	writeCDC(pdf, tr, de.Id, qrURL)
	writeReceptor(pdf, tr, de, rec)
	writeItems(pdf, tr, de.GDtipDE.GCamItem, moneda)
	if tot != nil {
		writeTotales(pdf, tr, *tot, moneda) // la remisión no informa gTotSub
	}
	if err := writeQR(pdf, qrURL); err != nil {
		return nil, err
	}
	writeLeyendas(pdf, tr)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("generar PDF KuDE: %w", err)
	}
	return buf.Bytes(), nil
}

func writeHeader(pdf *fpdf.Fpdf, tr func(string) string, e sifen.GEmis, t sifen.GTimb) {
	pdf.SetFont("Arial", "B", 13)
	pdf.CellFormat(120, 7, tr(e.DNomEmi), "", 0, "L", false, 0, "")

	// Recuadro de timbrado a la derecha.
	x, y := 135, 12.0
	pdf.SetXY(float64(x), y)
	pdf.SetFont("Arial", "B", 9)
	pdf.MultiCell(63, 4.5, tr(fmt.Sprintf(
		"RUC: %s-%d\nTimbrado Nº: %s\nInicio vigencia: %s\n%s\n%s-%s Nº %s",
		e.DRucEm, e.DDVEmi, t.DNumTim, t.DFeIniT, t.DDesTiDE, t.DEst, t.DPunExp, t.DNumDoc,
	)), "1", "L", false)

	pdf.SetXY(12, 20)
	pdf.SetFont("Arial", "", 9)
	dir := e.DDirEmi
	if e.DNumCas != "" && e.DNumCas != "0" {
		dir += " " + e.DNumCas
	}
	lineas := []string{
		"Dirección: " + dir,
	}
	if e.DTelEmi != "" {
		lineas = append(lineas, "Teléfono: "+e.DTelEmi)
	}
	if e.DEmailE != "" {
		lineas = append(lineas, "Email: "+e.DEmailE)
	}
	if len(e.GActEco) > 0 {
		lineas = append(lineas, "Actividad: "+e.GActEco[0].DDesActEco)
	}
	pdf.MultiCell(120, 4.5, tr(strings.Join(lineas, "\n")), "", "L", false)
	pdf.Ln(2)
}

func writeCDC(pdf *fpdf.Fpdf, tr func(string) string, cdc, _ string) {
	pdf.SetFont("Arial", "B", 8)
	pdf.CellFormat(0, 5, tr("CDC: "+formatCDC(cdc)), "T", 1, "L", false, 0, "")
}

func writeReceptor(pdf *fpdf.Fpdf, tr func(string) string, de *sifen.DE, r sifen.GDatRec) {
	pdf.SetFont("Arial", "", 9)
	id := "RUC " + r.DRucRec
	if r.DRucRec == "" {
		id = r.DDTipIDRec + " " + r.DNumIDRec
	}
	cond := "Contado"
	if de.GDtipDE.GCamCond != nil {
		cond = de.GDtipDE.GCamCond.DDCondOpe
	}
	pdf.MultiCell(0, 4.5, tr(fmt.Sprintf(
		"Fecha de emisión: %s\nCliente: %s\nIdentificación: %s\nCondición de venta: %s",
		de.GDatGralOpe.DFeEmiDE, r.DNomRec, id, cond,
	)), "", "L", false)
	pdf.Ln(1)
}

func writeItems(pdf *fpdf.Fpdf, tr func(string) string, items []sifen.GCamItem, moneda string) {
	pdf.SetFont("Arial", "B", 8)
	pdf.SetFillColor(230, 230, 230)
	headers := []struct {
		w float64
		t string
	}{
		{18, "Código"}, {78, "Descripción"}, {14, "Cant."},
		{28, "P. Unit."}, {14, "IVA %"}, {30, "Total"},
	}
	for _, h := range headers {
		pdf.CellFormat(h.w, 6, tr(h.t), "1", 0, "C", true, 0, "")
	}
	pdf.Ln(-1)

	pdf.SetFont("Arial", "", 8)
	for _, it := range items {
		total := effectiveTotOpe(it)
		var pUni, tasa string
		if it.GValorItem != nil {
			pUni = fmtMonto(it.GValorItem.DPUniProSer, moneda)
		}
		if it.GCamIVA != nil {
			tasa = it.GCamIVA.DTasaIVA.String()
		}
		pdf.CellFormat(18, 5, tr(it.DCodInt), "1", 0, "L", false, 0, "")
		pdf.CellFormat(78, 5, tr(truncate(it.DDesProSer, 60)), "1", 0, "L", false, 0, "")
		pdf.CellFormat(14, 5, tr(it.DCantProSer.String()), "1", 0, "R", false, 0, "")
		pdf.CellFormat(28, 5, tr(pUni), "1", 0, "R", false, 0, "")
		pdf.CellFormat(14, 5, tr(tasa), "1", 0, "R", false, 0, "")
		pdf.CellFormat(30, 5, tr(fmtMonto(total, moneda)), "1", 0, "R", false, 0, "")
		pdf.Ln(-1)
	}
	pdf.Ln(1)
}

func writeTotales(pdf *fpdf.Fpdf, tr func(string) string, t sifen.GTotSub, moneda string) {
	pdf.SetFont("Arial", "", 9)
	rows := [][2]string{
		{"Subtotal exento", fmtMonto(t.DSubExe, moneda)},
		{"Subtotal exonerado", fmtMonto(t.DSubExo, moneda)},
		{"Subtotal gravado 5%", fmtMonto(t.DSub5, moneda)},
		{"Subtotal gravado 10%", fmtMonto(t.DSub10, moneda)},
		{"Total de la operación", fmtMonto(t.DTotOpe, moneda)},
		{"Liquidación IVA 5%", fmtMonto(t.DTotIVA5, moneda)},
		{"Liquidación IVA 10%", fmtMonto(t.DTotIVA10, moneda)},
		{"Total IVA", fmtMonto(t.DTotIVA, moneda)},
	}
	for _, r := range rows {
		pdf.CellFormat(124, 5, "", "", 0, "L", false, 0, "")
		pdf.CellFormat(30, 5, tr(r[0]), "", 0, "R", false, 0, "")
		pdf.CellFormat(30, 5, tr(r[1]), "", 1, "R", false, 0, "")
	}
	pdf.SetFont("Arial", "B", 10)
	pdf.CellFormat(124, 6, "", "", 0, "L", false, 0, "")
	pdf.CellFormat(30, 6, tr("TOTAL GENERAL"), "T", 0, "R", false, 0, "")
	pdf.CellFormat(30, 6, tr(fmtMonto(t.DTotGralOpe, moneda)), "T", 1, "R", false, 0, "")
	if t.DTotalGs != nil {
		pdf.SetFont("Arial", "", 8)
		pdf.CellFormat(124, 5, "", "", 0, "L", false, 0, "")
		pdf.CellFormat(30, 5, tr("Total en Gs."), "", 0, "R", false, 0, "")
		pdf.CellFormat(30, 5, tr(fmtMonto(*t.DTotalGs, "PYG")), "", 1, "R", false, 0, "")
	}
	pdf.Ln(2)
}

func writeQR(pdf *fpdf.Fpdf, qrURL string) error {
	png, err := qrcode.Encode(qrURL, qrcode.Medium, 256)
	if err != nil {
		return fmt.Errorf("generar QR: %w", err)
	}
	opt := fpdf.ImageOptions{ImageType: "PNG", ReadDpi: false}
	pdf.RegisterImageOptionsReader("qr", opt, bytes.NewReader(png))
	y := pdf.GetY()
	pdf.ImageOptions("qr", 12, y, 32, 32, false, opt, 0, "")
	return nil
}

func writeLeyendas(pdf *fpdf.Fpdf, tr func(string) string) {
	y := pdf.GetY()
	pdf.SetXY(48, y)
	pdf.SetFont("Arial", "", 8)
	pdf.MultiCell(110, 4, tr(
		"Consulte la validez de este documento en:\n"+
			"https://ekuatia.set.gov.py/consultas-test\n"+
			"ingresando el CDC impreso arriba."), "", "L", false)
	pdf.Ln(3)
	pdf.SetFont("Arial", "B", 9)
	pdf.SetTextColor(180, 0, 0)
	pdf.MultiCell(0, 5, tr(leyendaPrueba), "1", "C", false)
	pdf.SetTextColor(0, 0, 0)
}

// --- helpers ---

func effectiveTotOpe(it sifen.GCamItem) decimal.Decimal {
	if it.GValorItem == nil {
		return decimal.Zero
	}
	if it.GValorItem.GValorRestaItem != nil {
		return it.GValorItem.GValorRestaItem.DTotOpeItem
	}
	return it.GValorItem.DTotBruOpeItem
}

func formatCDC(cdc string) string {
	var b strings.Builder
	for i, r := range cdc {
		if i > 0 && i%4 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// fmtMonto formatea un monto con separador de miles y decimales según la moneda.
func fmtMonto(v decimal.Decimal, moneda string) string {
	scale := int32(0)
	if !strings.EqualFold(moneda, "PYG") {
		scale = 2
	}
	v = v.Round(scale)
	neg := v.IsNegative()
	if neg {
		v = v.Neg()
	}
	str := v.StringFixed(scale)
	intPart := str
	decPart := ""
	if scale > 0 {
		if i := strings.IndexByte(str, '.'); i >= 0 {
			intPart = str[:i]
			decPart = str[i+1:]
		}
	}
	// Agrupar miles con punto.
	var grouped strings.Builder
	n := len(intPart)
	for i, c := range intPart {
		if i > 0 && (n-i)%3 == 0 {
			grouped.WriteByte('.')
		}
		grouped.WriteRune(c)
	}
	out := grouped.String()
	if scale > 0 {
		out += "," + decPart
	}
	if neg {
		out = "-" + out
	}
	return out
}
