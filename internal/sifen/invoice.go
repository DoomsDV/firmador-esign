package sifen

import (
	"fmt"
	"strconv"

	"github.com/shopspring/decimal"
)

// NewRDE crea un rDE con la cabecera de namespaces/versión y el CDC como Id del DE.
func NewRDE(idCDC string) *RDE {
	lastChar := idCDC[len(idCDC)-1:]
	dvId, _ := strconv.Atoi(lastChar)

	return &RDE{
		XmlNs:   SifenNS,
		Xsi:     SifenXSI,
		Schema:  SifenSchema,
		DVerFor: decimal.NewFromInt(Version),
		DE: &DE{
			Id:    idCDC,
			DDVId: dvId,
		},
	}
}

// ItemInput describe un ítem de venta antes de calcular su IVA (Manual E7/E8).
type ItemInput struct {
	Codigo         string
	Descripcion    string
	Cantidad       decimal.Decimal
	PrecioUnitario decimal.Decimal

	// Unidad de medida (cUniMed). 0 => 77 (UNI).
	UnidadMedida    int
	DesUnidadMedida string

	// AfectacionIVA: 1 gravado, 2 exonerado, 3 exento, 4 gravado parcial.
	AfectacionIVA int
	// TasaIVA: 10, 5 o 0 (exento/exonerado).
	TasaIVA int
	// PropIVA: proporción gravada (%) para gravado parcial. 0 => 100 si gravado total.
	PropIVA decimal.Decimal
}

// afecIVADesc mapea la afectación a su descripción oficial (dDesAfecIVA).
func afecIVADesc(afec int) (string, error) {
	switch afec {
	case AfecIVAGravado:
		return "Gravado IVA", nil
	case AfecIVAExonerado:
		return "Exonerado (Art. 100 - Ley 6380/2019)", nil
	case AfecIVAExento:
		return "Exento", nil
	case AfecIVAParcial:
		return "Gravado parcial (Grav-Exento)", nil
	default:
		return "", fmt.Errorf("iAfecIVA inválido: %d", afec)
	}
}

// computeIVA calcula base gravada, IVA liquidado y base exenta de un ítem según
// su afectación y tasa, con redondeo acorde a la moneda.
func computeIVA(totalOpe decimal.Decimal, afec, tasa int, prop decimal.Decimal, moneda string) (propIVA, basGrav, liqIVA, basExe decimal.Decimal, err error) {
	tasaDec := decimal.NewFromInt(int64(tasa))
	switch afec {
	case AfecIVAGravado:
		if tasa != 5 && tasa != 10 {
			return cero, cero, cero, cero, fmt.Errorf("gravado requiere tasa 5 o 10, got %d", tasa)
		}
		liqIVA = roundMonto(totalOpe.Mul(tasaDec).Div(cien.Add(tasaDec)), moneda)
		basGrav = totalOpe.Sub(liqIVA)
		return cien, basGrav, liqIVA, cero, nil

	case AfecIVAExonerado:
		// E737 dBasExe = 0 para E731 = 1, 2 o 3 (NT13). El subtotal exonerado
		// (dSubExo) se agrega desde EA008/dTotOpeItem, no desde dBasExe.
		return cero, cero, cero, cero, nil

	case AfecIVAExento:
		// E737 dBasExe = 0 para E731 = 1, 2 o 3 (NT13). El subtotal exento
		// (dSubExe) se agrega desde EA008/dTotOpeItem, no desde dBasExe.
		return cero, cero, cero, cero, nil

	case AfecIVAParcial:
		if tasa != 5 && tasa != 10 {
			return cero, cero, cero, cero, fmt.Errorf("gravado parcial requiere tasa 5 o 10, got %d", tasa)
		}
		if !prop.IsPositive() || prop.GreaterThanOrEqual(cien) {
			return cero, cero, cero, cero, fmt.Errorf("gravado parcial requiere dPropIVA en (0,100), got %s", prop)
		}
		// NT13 (E735/E737): denom = 10000 + (E734 * E733)
		//   dBasGravIVA = [100 * EA008 * E733] / denom
		//   dBasExe     = [100 * EA008 * (100 - E733)] / denom
		//   dLiqIVAItem = dBasGravIVA * (E734/100)
		denom := cien.Mul(cien).Add(tasaDec.Mul(prop))
		basGrav = roundMonto(cien.Mul(totalOpe).Mul(prop).Div(denom), moneda)
		basExe = roundMonto(cien.Mul(totalOpe).Mul(cien.Sub(prop)).Div(denom), moneda)
		liqIVA = roundMonto(basGrav.Mul(tasaDec).Div(cien), moneda)
		return prop, basGrav, liqIVA, basExe, nil

	default:
		return cero, cero, cero, cero, fmt.Errorf("iAfecIVA inválido: %d", afec)
	}
}

// BuildItem construye un GCamItem completo (valor + IVA) a partir de un ItemInput.
func BuildItem(in ItemInput, moneda string) (GCamItem, error) {
	if in.Codigo == "" {
		return GCamItem{}, fmt.Errorf("ítem sin código")
	}
	if in.Descripcion == "" {
		return GCamItem{}, fmt.Errorf("ítem %q sin descripción", in.Codigo)
	}
	if !in.Cantidad.IsPositive() {
		return GCamItem{}, fmt.Errorf("ítem %q requiere cantidad > 0", in.Codigo)
	}
	if in.PrecioUnitario.IsNegative() {
		return GCamItem{}, fmt.Errorf("ítem %q con precio negativo", in.Codigo)
	}

	desAfec, err := afecIVADesc(in.AfectacionIVA)
	if err != nil {
		return GCamItem{}, fmt.Errorf("ítem %q: %w", in.Codigo, err)
	}

	totalBruto := roundMonto(in.PrecioUnitario.Mul(in.Cantidad), moneda)

	propIVA, basGrav, liqIVA, basExe, err := computeIVA(totalBruto, in.AfectacionIVA, in.TasaIVA, in.PropIVA, moneda)
	if err != nil {
		return GCamItem{}, fmt.Errorf("ítem %q: %w", in.Codigo, err)
	}

	uni := in.UnidadMedida
	desUni := in.DesUnidadMedida
	if uni == 0 {
		uni = 77
		desUni = "UNI"
	}
	if desUni == "" {
		desUni = "UNI"
	}

	return GCamItem{
		DCodInt:     in.Codigo,
		DDesProSer:  in.Descripcion,
		CUniMed:     uni,
		DDesUniMed:  desUni,
		DCantProSer: in.Cantidad,
		GValorItem: &GValorItem{
			DPUniProSer:    in.PrecioUnitario,
			DTotBruOpeItem: totalBruto,
			GValorRestaItem: &GValorRestaItem{
				DDescItem:    cero,
				DPorcDesIt:   cero,
				DDescGloItem: cero,
				DTotOpeItem:  totalBruto,
			},
		},
		GCamIVA: &GCamIVA{
			IAfecIVA:    in.AfectacionIVA,
			DDesAfecIVA: desAfec,
			DPropIVA:    propIVA,
			DTasaIVA:    decimal.NewFromInt(int64(in.TasaIVA)),
			DBasGravIVA: basGrav,
			DLiqIVAItem: liqIVA,
			DBasExe:     basExe,
		},
	}, nil
}

// BuildItemRemision construye un ítem de nota de remisión (C002=7): solo campos
// descriptivos y cantidad; sin gValorItem ni gCamIVA (Manual E700, reglas E730/EA
// no aplican a la remisión).
func BuildItemRemision(in ItemInput) (GCamItem, error) {
	if in.Codigo == "" {
		return GCamItem{}, fmt.Errorf("ítem sin código")
	}
	if in.Descripcion == "" {
		return GCamItem{}, fmt.Errorf("ítem %q sin descripción", in.Codigo)
	}
	if !in.Cantidad.IsPositive() {
		return GCamItem{}, fmt.Errorf("ítem %q requiere cantidad > 0", in.Codigo)
	}
	uni := in.UnidadMedida
	desUni := in.DesUnidadMedida
	if uni == 0 {
		uni = 77
		desUni = "UNI"
	}
	if desUni == "" {
		desUni = "UNI"
	}
	return GCamItem{
		DCodInt:     in.Codigo,
		DDesProSer:  in.Descripcion,
		CUniMed:     uni,
		DDesUniMed:  desUni,
		DCantProSer: in.Cantidad,
	}, nil
}

// AgregarItem construye y agrega un ítem al DE. La moneda determina el redondeo.
func (de *DE) AgregarItem(in ItemInput, moneda string) error {
	item, err := BuildItem(in, moneda)
	if err != nil {
		return err
	}
	if de.GDtipDE.GCamItem == nil {
		de.GDtipDE.GCamItem = make([]GCamItem, 0, 1)
	}
	de.GDtipDE.GCamItem = append(de.GDtipDE.GCamItem, item)
	return nil
}
