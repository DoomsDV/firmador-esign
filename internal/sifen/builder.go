package sifen

import (
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// tipoDEDesc mapea iTiDE a su descripción oficial (dDesTiDE, Manual C002).
var tipoDEDesc = map[int]string{
	1: "Factura electrónica",
	4: "Autofactura electrónica",
	5: "Nota de crédito electrónica",
	6: "Nota de débito electrónica",
	7: "Nota de remisión electrónica",
}

// DescTipoDE devuelve la descripción oficial de un tipo de DE.
func DescTipoDE(tipo int) (string, error) {
	d, ok := tipoDEDesc[tipo]
	if !ok {
		return "", fmt.Errorf("iTiDE no soportado: %d", tipo)
	}
	return d, nil
}

// Emisor agrupa los datos del emisor del DE (gEmis).
type Emisor struct {
	RUC               string
	DV                int
	TipoContribuyente int // iTipCont: 1 física, 2 jurídica (también alimenta el CDC)
	TipoRegimen       int // cTipReg (opcional 1-8; 0 = no informar)
	Nombre            string
	NombreFantasia    string
	Direccion         string
	NumeroCasa        string
	Departamento      int
	DesDepartamento   string
	Distrito          int
	DesDistrito       string
	Ciudad            int
	DesCiudad         string
	Telefono          string
	Email             string
	Actividades       []GActEco
}

// DocumentInput describe todo lo necesario para construir cualquier tipo de DE.
type DocumentInput struct {
	TipoDE          int    // iTiDE: 1 FE, 4 autofactura, 5 NCE, 6 NDE, 7 remisión
	Establecimiento string // dEst, ej. "001"
	PuntoExp        string // dPunExp, ej. "001"
	NumeroDoc       int    // dNumDoc
	NumTimbrado     string // dNumTim (8 dígitos)
	FeIniT          string // dFeIniT (YYYY-MM-DD)
	TipoEmision     int    // iTipEmi: 1 normal, 2 contingencia
	CodigoSeguridad int    // dCodSeg (9 dígitos); 0 => generar

	FechaFirma time.Time // en zona horaria de Asunción

	Emisor   Emisor
	Receptor GDatRec

	// Operación comercial (gOpeCom).
	TipoTransaccion     int              // iTipTra
	DesTipoTransaccion  string           // dDesTipTra
	Moneda              string           // cMoneOpe; "" => PYG
	DesMoneda           string           // dDesMoneOpe
	CondicionTipoCambio int              // dCondTiCam (1 global, 2 por ítem); requerido si moneda ≠ PYG
	TipoCambio          *decimal.Decimal // dTiCam; requerido si moneda ≠ PYG y dCondTiCam=1

	Items     []ItemInput
	Condicion *GCamCond // gCamCond (contado/crédito); requerido en FE/NCE/NDE

	// Campos específicos de factura electrónica (gCamFE).
	IndPres    int    // iIndPres
	DesIndPres string // dDesIndPres

	// Documento(s) asociado(s) para NCE/NDE (gCamDEAsoc) y motivo (gCamNCDE).
	MotivoEmision int          // iMotEmi (NCE/NDE)
	DocsAsociados []GCamDEAsoc // gCamDEAsoc

	// Grupos específicos de autofactura (tipoDE=4) y nota de remisión (tipoDE=7).
	CamAE      *GCamAE  // gCamAE (autofactura)
	CamNRE     *GCamNRE // gCamNRE (remisión)
	Transporte *GTransp // gTransp (remisión)
}

// BuildDE valida el input y arma el rDE completo (sin firmar). La firma y el QR
// los agrega el llamador con FirmarYSerializar.
func BuildDE(in DocumentInput) (*RDE, error) {
	if err := validateDocumentInput(&in); err != nil {
		return nil, err
	}

	desTiDE, err := DescTipoDE(in.TipoDE)
	if err != nil {
		return nil, err
	}

	codSeg := in.CodigoSeguridad
	if codSeg == 0 {
		codSeg = GenerateSecurityCode()
	}

	feEmi := in.FechaFirma.Format("2006-01-02T15:04:05")
	moneda := in.Moneda

	cdc := GenerateCDC(
		in.TipoDE,
		in.Emisor.RUC,
		in.Emisor.DV,
		atoiTrim(in.Establecimiento),
		atoiTrim(in.PuntoExp),
		in.NumeroDoc,
		in.Emisor.TipoContribuyente,
		in.FechaFirma,
		in.TipoEmision,
		codSeg,
	)

	rde := NewRDE(cdc)
	rde.DE.DFecFirma = feEmi
	rde.DE.DSisFact = 1

	rde.DE.GOpeDE = GOpeDE{
		ITipEmi:    in.TipoEmision,
		DDesTipEmi: tipoEmisionDesc(in.TipoEmision),
		DCodSeg:    fmt.Sprintf("%09d", codSeg),
	}

	rde.DE.GTimb = GTimb{
		ITiDE:    in.TipoDE,
		DDesTiDE: desTiDE,
		DNumTim:  in.NumTimbrado,
		DFeIniT:  in.FeIniT,
		DEst:     in.Establecimiento,
		DPunExp:  in.PuntoExp,
		DNumDoc:  fmt.Sprintf("%07d", in.NumeroDoc),
	}

	gdgo := GDatGralOpe{
		DFeEmiDE: feEmi,
		GEmis:    buildEmis(in.Emisor),
		GDatRec:  in.Receptor,
	}
	// gOpeCom (D010) no se informa en nota de remisión (C002=7, validación 1201).
	if in.TipoDE != 7 {
		gdgo.GOpeCom = buildOpeCom(in)
	}
	rde.DE.GDatGralOpe = gdgo

	items := make([]GCamItem, 0, len(in.Items))
	for _, it := range in.Items {
		var (
			item GCamItem
			err  error
		)
		if in.TipoDE == 7 {
			item, err = BuildItemRemision(it) // remisión: sin gValorItem ni gCamIVA
		} else {
			item, err = BuildItem(it, moneda)
			// Autofactura (C002=4): lleva gValorItem pero no gCamIVA (validación 1901).
			if err == nil && in.TipoDE == 4 {
				item.GCamIVA = nil
			}
		}
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	gdtip := GDtipDE{
		GCamCond: in.Condicion,
		GCamItem: items,
	}
	if err := applyTipoDECampos(&gdtip, in); err != nil {
		return nil, err
	}
	rde.DE.GDtipDE = gdtip
	// gCamDEAsoc: obligatorio en autofactura/NCE/NDE (C002=4,5,6) y opcional en
	// FE/remisión (C002=1,7).
	if len(in.DocsAsociados) > 0 {
		rde.DE.GCamDEAsoc = in.DocsAsociados
	}

	// gTotSub no se informa en nota de remisión (C002=7).
	if in.TipoDE != 7 {
		var tot GTotSub
		if in.TipoDE == 4 {
			// Autofactura: sin IVA; dTotOpe = dTotGralOpe = Σ dTotOpeItem.
			tot = calcularTotalesAutofactura(items, moneda)
		} else {
			tot = CalcularTotales(items, moneda)
		}
		if !isPYG(moneda) {
			if in.TipoCambio != nil {
				totGs := roundMonto(tot.DTotGralOpe.Mul(*in.TipoCambio), "PYG")
				tot.DTotalGs = &totGs
			}
		}
		rde.DE.GTotSub = &tot
	}

	return rde, nil
}

// applyTipoDECampos agrega los grupos específicos según el tipo de DE.
func applyTipoDECampos(gdtip *GDtipDE, in DocumentInput) error {
	switch in.TipoDE {
	case 1: // factura electrónica
		gdtip.GCamFE = &GCamFE{
			IIndPres:    in.IndPres,
			DDesIndPres: in.DesIndPres,
		}
	case 5, 6: // NCE / NDE
		desMot, err := motivoEmisionDesc(in.MotivoEmision)
		if err != nil {
			return err
		}
		gdtip.GCamNCDE = &GCamNCDE{
			IMotEmi:    in.MotivoEmision,
			DDesMotEmi: desMot,
		}
		if len(in.DocsAsociados) == 0 {
			return fmt.Errorf("NCE/NDE requiere al menos un documento asociado (gCamDEAsoc)")
		}
	case 4: // autofactura
		if in.CamAE == nil {
			return fmt.Errorf("autofactura requiere gCamAE (datos del vendedor)")
		}
		if len(in.DocsAsociados) == 0 {
			return fmt.Errorf("autofactura requiere un documento asociado (gCamDEAsoc, p. ej. constancia)")
		}
		gdtip.GCamAE = in.CamAE
	case 7: // nota de remisión
		if in.CamNRE == nil {
			return fmt.Errorf("nota de remisión requiere gCamNRE (motivo del traslado)")
		}
		if in.Transporte == nil {
			return fmt.Errorf("nota de remisión requiere gTransp (datos del transporte)")
		}
		gdtip.GCamNRE = in.CamNRE
		gdtip.GTransp = in.Transporte
	default:
		return fmt.Errorf("tipo de DE %d aún no soportado por BuildDE", in.TipoDE)
	}
	return nil
}

func buildOpeCom(in DocumentInput) *GOpeCom {
	moneda := in.Moneda
	desMon := in.DesMoneda
	if desMon == "" && isPYG(moneda) {
		desMon = "Guarani"
	}
	oc := &GOpeCom{
		ITImp:       1,
		DDesTImp:    "IVA",
		CMoneOpe:    moneda,
		DDesMoneOpe: desMon,
	}
	// iTipTra/dDesTipTra (D011/D012) solo en FE (1) y autofactura (4).
	if in.TipoDE == 1 || in.TipoDE == 4 {
		oc.ITipTra = in.TipoTransaccion
		oc.DDesTipTra = in.DesTipoTransaccion
	}
	if !isPYG(moneda) {
		oc.DCondTiCam = in.CondicionTipoCambio
		oc.DTiCam = in.TipoCambio
	}
	return oc
}

func buildEmis(e Emisor) GEmis {
	return GEmis{
		DRucEm:     e.RUC,
		DDVEmi:     e.DV,
		ITipCont:   e.TipoContribuyente,
		CTipReg:    e.TipoRegimen,
		DNomEmi:    e.Nombre,
		DNomFanEmi: e.NombreFantasia,
		DDirEmi:    e.Direccion,
		DNumCas:    e.NumeroCasa,
		CDepEmi:    e.Departamento,
		DDesDepEmi: e.DesDepartamento,
		CDisEmi:    e.Distrito,
		DDesDisEmi: e.DesDistrito,
		CCiuEmi:    e.Ciudad,
		DDesCiuEmi: e.DesCiudad,
		DTelEmi:    e.Telefono,
		DEmailE:    e.Email,
		GActEco:    e.Actividades,
	}
}

func validateDocumentInput(in *DocumentInput) error {
	if in.Emisor.RUC == "" {
		return fmt.Errorf("emisor sin RUC")
	}
	if in.Emisor.TipoContribuyente != 1 && in.Emisor.TipoContribuyente != 2 {
		return fmt.Errorf("emisor con iTipCont inválido: %d", in.Emisor.TipoContribuyente)
	}
	if in.NumTimbrado == "" {
		return fmt.Errorf("falta dNumTim")
	}
	if in.FeIniT == "" {
		return fmt.Errorf("falta dFeIniT")
	}
	if in.NumeroDoc <= 0 {
		return fmt.Errorf("dNumDoc inválido: %d", in.NumeroDoc)
	}
	if in.TipoEmision == 0 {
		in.TipoEmision = 1
	}
	if in.Establecimiento == "" {
		in.Establecimiento = "001"
	}
	if in.PuntoExp == "" {
		in.PuntoExp = "001"
	}
	if in.Moneda == "" {
		in.Moneda = "PYG"
	}
	if len(in.Items) == 0 {
		return fmt.Errorf("el documento no tiene ítems")
	}
	if err := ValidateReceptor(in.Receptor); err != nil {
		return fmt.Errorf("receptor inválido: %w", err)
	}
	if !isPYG(in.Moneda) {
		if in.CondicionTipoCambio == 0 {
			return fmt.Errorf("moneda %s requiere dCondTiCam (1 global / 2 por ítem)", in.Moneda)
		}
		if in.CondicionTipoCambio == 1 && (in.TipoCambio == nil || !in.TipoCambio.IsPositive()) {
			return fmt.Errorf("moneda %s con dCondTiCam=1 requiere dTiCam > 0", in.Moneda)
		}
	}
	return nil
}

func tipoEmisionDesc(t int) string {
	if t == 2 {
		return "Contingencia"
	}
	return "Normal"
}

func isPYG(moneda string) bool {
	return moneda == "" || strings.EqualFold(moneda, "PYG")
}

func atoiTrim(s string) int {
	return atoiDefault(strings.TrimLeft(strings.TrimSpace(s), "0"), 0)
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
	}
	return n
}
