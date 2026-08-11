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
	"time"

	"github.com/shopspring/decimal"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/DoomsDV/firmador-e/internal/sifen"
)

// leyendaPrueba es la advertencia obligatoria en ambiente de homologación.
const leyendaPrueba = "DOCUMENTO GENERADO EN AMBIENTE DE PRUEBA - SIN VALOR COMERCIAL NI FISCAL"

// leyendaLegalKuDE es la leyenda normativa del pie (Manual Tecnico / preview del panel).
const leyendaLegalKuDE = "ESTE DOCUMENTO ES UNA REPRESENTACIÓN GRÁFICA DE UN DOCUMENTO ELECTRÓNICO (XML)"

const (
	TemplateMinimalista = "minimalista"
	TemplateCorporativa = "corporativa"
)

//go:embed templates/*.html
var templatesFS embed.FS

// Branding son las preferencias visuales del cliente (client_kude_config), leídas
// por Go a través del contexto del tenant (tenant.KudeConfig).
type Branding struct {
	TemplateID      string
	ColorPrimario   string
	LogoURL         string
	NotasFooter     string
	MostrarFantasia bool
}

// KudeData es el modelo, ya formateado para mostrar, que consume la plantilla HTML.
// Se construye una sola vez (BuildKudeData) a partir del rDE firmado; de ahí en más
// es un valor inmutable seguro de pasar entre goroutines (a diferencia del *RDE).
type KudeData struct {
	TemplateID           string
	ColorPrimario        string
	LogoURL              string
	NotasFooter          string
	MostrarFantasia      bool
	MostrarLeyendaPrueba bool
	LeyendaPrueba string

	CDC          string // formateado en grupos de 4
	FechaEmision string
	Condicion    string
	Moneda       string
	MonedaDesc   string
	TipoCambio   string // "" si moneda PYG o no informado

	Emisor   KudeEmisor
	Timbrado KudeTimbrado
	Receptor KudeReceptor
	Items    []KudeItem
	Totales  *KudeTotales // nil en nota de remisión (no lleva gTotSub)

	QRURL    string
	QRBase64 string // imagen PNG del QR, en base64 (sin el prefijo data:)
	URLConsulta string // URL del portal e-Kuatia (sin query del QR)
	LeyendaLegal string
}

type KudeEmisor struct {
	Nombre     string
	Fantasia   string
	RUC        string
	Direccion  string
	Ciudad     string // ciudad emisor (minimalista: pie de membrete derecho)
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
	IVAHint        string // texto inline en descripción: "IVA 10%", "Exento", etc.
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
	monedaDesc := "Guarani"
	var tipoCambio string
	if de.GDatGralOpe.GOpeCom != nil {
		oc := de.GDatGralOpe.GOpeCom
		if oc.CMoneOpe != "" {
			moneda = oc.CMoneOpe
		}
		if oc.DDesMoneOpe != "" {
			monedaDesc = oc.DDesMoneOpe
		}
		if oc.DTiCam != nil && !strings.EqualFold(moneda, "PYG") {
			tipoCambio = oc.DTiCam.StringFixed(2)
		}
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

	condicion := ""
	if de.GDtipDE.GCamCond != nil {
		condicion = de.GDtipDE.GCamCond.DDCondOpe
	}

	items := make([]KudeItem, 0, len(de.GDtipDE.GCamItem))
	for _, it := range de.GDtipDE.GCamItem {
		var pUni, tasa, ivaHint string
		if it.GValorItem != nil {
			pUni = fmtMonto(it.GValorItem.DPUniProSer, moneda)
		}
		if it.GCamIVA != nil {
			tasa = it.GCamIVA.DTasaIVA.String()
			switch it.GCamIVA.IAfecIVA {
			case 1, 4:
				if it.GCamIVA.DTasaIVA.IsPositive() {
					ivaHint = "IVA " + tasa + "%"
				}
			case 2:
				ivaHint = "Exonerado"
			case 3:
				ivaHint = "Exento"
			}
		}
		items = append(items, KudeItem{
			Codigo:         it.DCodInt,
			Descripcion:    it.DDesProSer,
			Cantidad:       it.DCantProSer.String(),
			PrecioUnitario: pUni,
			TasaIVA:        tasa,
			IVAHint:        ivaHint,
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

	id := ""
	if rec.DRucRec != "" {
		id = fmt.Sprintf("RUC %s-%d", rec.DRucRec, rec.DDVRec)
	} else if rec.DNumIDRec != "" {
		id = strings.TrimSpace(rec.DDTipIDRec + " " + rec.DNumIDRec)
	}

	data := KudeData{
		TemplateID:           templateID,
		ColorPrimario:        color,
		LogoURL:              strings.TrimSpace(b.LogoURL),
		NotasFooter:          strings.TrimSpace(b.NotasFooter),
		MostrarFantasia:      b.MostrarFantasia,
		MostrarLeyendaPrueba: !strings.EqualFold(strings.TrimSpace(ambiente), "prod"),
		LeyendaPrueba:        leyendaPrueba,
		CDC:                  formatCDC(de.Id),
		FechaEmision:         fmtFechaHoraDE(de.GDatGralOpe.DFeEmiDE),
		Condicion:            condicion,
		Moneda:               moneda,
		MonedaDesc:           monedaDesc,
		TipoCambio:           tipoCambio,
		Emisor: KudeEmisor{
			Nombre:    emis.DNomEmi,
			Fantasia:  emis.DNomFanEmi,
			RUC:       fmt.Sprintf("%s-%d", emis.DRucEm, emis.DDVEmi),
			Direccion: emisDireccion(emis),
			Ciudad:    emisCiudad(emis),
			Telefono:  emis.DTelEmi,
			Email:     emis.DEmailE,
			Actividad: emisActividad(emis),
		},
		Timbrado: KudeTimbrado{
			NumTimbrado:     timb.DNumTim,
			FeIniT:          fmtFechaDE(timb.DFeIniT),
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
		URLConsulta: urlConsultaDisplay(qrURL),
		LeyendaLegal: leyendaLegalKuDE,
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

func emisCiudad(e sifen.GEmis) string {
	parts := make([]string, 0, 2)
	if e.DDesCiuEmi != "" {
		parts = append(parts, e.DDesCiuEmi)
	}
	if e.DDesDepEmi != "" && !strings.EqualFold(e.DDesDepEmi, e.DDesCiuEmi) {
		parts = append(parts, e.DDesDepEmi)
	}
	return strings.Join(parts, ", ")
}

func urlConsultaDisplay(qrURL string) string {
	u := strings.TrimSpace(qrURL)
	if i := strings.Index(u, "?"); i > 0 {
		u = u[:i]
	}
	if strings.HasSuffix(strings.ToLower(u), "/qr") {
		u = u[:len(u)-3] + "/"
	}
	return u
}

func fmtFechaDE(iso string) string {
	iso = strings.TrimSpace(iso)
	if iso == "" {
		return iso
	}
	if t, err := time.Parse("2006-01-02", iso); err == nil {
		return t.Format("02/01/2006")
	}
	return iso
}

func fmtFechaHoraDE(iso string) string {
	iso = strings.TrimSpace(iso)
	if iso == "" {
		return iso
	}
	layouts := []string{
		"2006-01-02T15:04:05",
		time.RFC3339,
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, iso); err == nil {
			if layout == "2006-01-02" {
				return t.Format("02/01/2006")
			}
			return t.Format("02/01/2006 15:04:05")
		}
	}
	return iso
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
