// Package kude arma los datos y el HTML de la representación gráfica (KuDE) de un
// documento electrónico, con branding por cliente (plantilla/color/logo). El HTML
// resultante se convierte a PDF externamente vía Gotenberg (ver internal/gotenberg);
// este paquete no genera bytes de PDF.
package kude

import (
	"bytes"
	"embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"strings"

	"github.com/shopspring/decimal"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/DoomsDV/firmador-e/internal/sifen"
)

// leyendaPrueba es la advertencia obligatoria en ambiente de homologación.
const leyendaPrueba = "DOCUMENTO GENERADO EN AMBIENTE DE PRUEBA - SIN VALOR COMERCIAL NI FISCAL"

const (
	TemplateMinimalista = "minimalista"
	TemplateCorporativa = "corporativa"
)

//go:embed templates/*.html
var templatesFS embed.FS

// Branding son las preferencias visuales del cliente (client_kude_config), leídas
// por Go a través del contexto del tenant (tenant.KudeConfig).
type Branding struct {
	TemplateID    string
	ColorPrimario string
	LogoURL       string
	NotasFooter   string
}

// KudeData es el modelo, ya formateado para mostrar, que consume la plantilla HTML.
// Se construye una sola vez (BuildKudeData) a partir del rDE firmado; de ahí en más
// es un valor inmutable seguro de pasar entre goroutines (a diferencia del *RDE).
type KudeData struct {
	TemplateID    string
	ColorPrimario string
	LogoURL       string
	NotasFooter   string
	MostrarLeyendaPrueba bool
	LeyendaPrueba string

	CDC          string // formateado en grupos de 4
	FechaEmision string
	Condicion    string

	Emisor   KudeEmisor
	Timbrado KudeTimbrado
	Receptor KudeReceptor
	Items    []KudeItem
	Totales  *KudeTotales // nil en nota de remisión (no lleva gTotSub)

	QRURL    string
	QRBase64 string // imagen PNG del QR, en base64 (sin el prefijo data:)
}

type KudeEmisor struct {
	Nombre     string
	Fantasia   string
	RUC        string
	Direccion  string
	Telefono   string
	Email      string
	Actividad  string
}

type KudeTimbrado struct {
	NumTimbrado     string
	FeIniT          string
	TipoDocDesc     string
	Establecimiento string
	PuntoExp        string
	NumeroDoc       string
}

type KudeReceptor struct {
	Nombre         string
	Identificacion string
	Direccion      string // "" si el DE no la informa (dDirRec es omitempty)
	Telefono       string
	Email          string
}

type KudeItem struct {
	Codigo         string
	Descripcion    string
	Cantidad       string
	PrecioUnitario string
	TasaIVA        string
	Total          string
}

type KudeTotales struct {
	Moneda     string
	SubExe     string
	SubExo     string
	Sub5       string
	Sub10      string
	TotOpe     string
	TotIVA5    string
	TotIVA10   string
	TotIVA     string
	TotGralOpe string
	TotalGs    string // "" si la moneda es PYG
}

// BuildKudeData extrae del rDE firmado (y del branding del tenant) el modelo listo
// para renderizar. No muta rde; el resultado es inmutable y puede cruzarse a otra
// goroutine sin compartir el *etree.Document del pipeline de firma.
func BuildKudeData(rde *sifen.RDE, qrURL string, ambiente string, b Branding) (KudeData, error) {
	if rde == nil || rde.DE == nil {
		return KudeData{}, fmt.Errorf("rDE/DE nulo")
	}
	if qrURL == "" {
		return KudeData{}, fmt.Errorf("qrURL vacío: el KuDE requiere el dCarQR")
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

	qrPNG, err := qrcode.Encode(qrURL, qrcode.Medium, 256)
	if err != nil {
		return KudeData{}, fmt.Errorf("generar QR: %w", err)
	}

	templateID := strings.TrimSpace(b.TemplateID)
	if templateID != TemplateCorporativa {
		templateID = TemplateMinimalista
	}
	color := strings.TrimSpace(b.ColorPrimario)
	if color == "" {
		color = "#0f172a"
	}

	condicion := "Contado"
	if de.GDtipDE.GCamCond != nil {
		condicion = de.GDtipDE.GCamCond.DDCondOpe
	}

	items := make([]KudeItem, 0, len(de.GDtipDE.GCamItem))
	for _, it := range de.GDtipDE.GCamItem {
		var pUni, tasa string
		if it.GValorItem != nil {
			pUni = fmtMonto(it.GValorItem.DPUniProSer, moneda)
		}
		if it.GCamIVA != nil {
			tasa = it.GCamIVA.DTasaIVA.String()
		}
		items = append(items, KudeItem{
			Codigo:         it.DCodInt,
			Descripcion:    it.DDesProSer,
			Cantidad:       it.DCantProSer.String(),
			PrecioUnitario: pUni,
			TasaIVA:        tasa,
			Total:          fmtMonto(effectiveTotOpe(it), moneda),
		})
	}

	var totales *KudeTotales
	if tot != nil {
		totales = &KudeTotales{
			Moneda:     moneda,
			SubExe:     fmtMonto(tot.DSubExe, moneda),
			SubExo:     fmtMonto(tot.DSubExo, moneda),
			Sub5:       fmtMonto(tot.DSub5, moneda),
			Sub10:      fmtMonto(tot.DSub10, moneda),
			TotOpe:     fmtMonto(tot.DTotOpe, moneda),
			TotIVA5:    fmtMonto(tot.DTotIVA5, moneda),
			TotIVA10:   fmtMonto(tot.DTotIVA10, moneda),
			TotIVA:     fmtMonto(tot.DTotIVA, moneda),
			TotGralOpe: fmtMonto(tot.DTotGralOpe, moneda),
		}
		if tot.DTotalGs != nil {
			totales.TotalGs = fmtMonto(*tot.DTotalGs, "PYG")
		}
	}

	id := "RUC " + rec.DRucRec
	if rec.DRucRec == "" {
		id = rec.DDTipIDRec + " " + rec.DNumIDRec
	}

	data := KudeData{
		TemplateID:           templateID,
		ColorPrimario:        color,
		LogoURL:              strings.TrimSpace(b.LogoURL),
		NotasFooter:          strings.TrimSpace(b.NotasFooter),
		MostrarLeyendaPrueba: !strings.EqualFold(strings.TrimSpace(ambiente), "prod"),
		LeyendaPrueba:        leyendaPrueba,
		CDC:                  formatCDC(de.Id),
		FechaEmision:         de.GDatGralOpe.DFeEmiDE,
		Condicion:            condicion,
		Emisor: KudeEmisor{
			Nombre:    emis.DNomEmi,
			Fantasia:  emis.DNomFanEmi,
			RUC:       fmt.Sprintf("%s-%d", emis.DRucEm, emis.DDVEmi),
			Direccion: emisDireccion(emis),
			Telefono:  emis.DTelEmi,
			Email:     emis.DEmailE,
			Actividad: emisActividad(emis),
		},
		Timbrado: KudeTimbrado{
			NumTimbrado:     timb.DNumTim,
			FeIniT:          timb.DFeIniT,
			TipoDocDesc:     timb.DDesTiDE,
			Establecimiento: timb.DEst,
			PuntoExp:        timb.DPunExp,
			NumeroDoc:       timb.DNumDoc,
		},
		Receptor: KudeReceptor{
			Nombre:         rec.DNomRec,
			Identificacion: id,
			Direccion:      recDireccion(rec),
			Telefono:       rec.DTelRec,
			Email:          rec.DEmailRec,
		},
		Items:    items,
		Totales:  totales,
		QRURL:    qrURL,
		QRBase64: base64.StdEncoding.EncodeToString(qrPNG),
	}
	return data, nil
}

// RenderKuDEHTML ejecuta la plantilla Go (minimalista/corporativa) con los datos ya
// formateados y devuelve el HTML final, listo para enviarse a Gotenberg.
func RenderKuDEHTML(data KudeData) (string, error) {
	name := data.TemplateID
	if name != TemplateCorporativa {
		name = TemplateMinimalista
	}
	tmpl, err := template.ParseFS(templatesFS, "templates/"+name+".html")
	if err != nil {
		return "", fmt.Errorf("parsear plantilla %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("renderizar plantilla %s: %w", name, err)
	}
	return buf.String(), nil
}

// --- helpers ---

func emisDireccion(e sifen.GEmis) string {
	dir := e.DDirEmi
	if e.DNumCas != "" && e.DNumCas != "0" {
		dir += " " + e.DNumCas
	}
	return dir
}

func recDireccion(r sifen.GDatRec) string {
	dir := r.DDirRec
	if r.DNumCasRec != "" && r.DNumCasRec != "0" {
		dir += " " + r.DNumCasRec
	}
	return strings.TrimSpace(dir)
}

func emisActividad(e sifen.GEmis) string {
	if len(e.GActEco) == 0 {
		return ""
	}
	return e.GActEco[0].DDesActEco
}

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
