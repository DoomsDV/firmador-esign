package sifen

import (
	"strings"

	"github.com/shopspring/decimal"
)

// Afectación tributaria del IVA por ítem (Manual Técnico, campo E731 iAfecIVA).
const (
	AfecIVAGravado   = 1 // gravado total
	AfecIVAExonerado = 2 // exonerado (art. 100 Ley 6380)
	AfecIVAExento    = 3 // exento
	AfecIVAParcial   = 4 // gravado parcial (gravado + exento en el mismo ítem)
)

var (
	cien  = decimal.NewFromInt(100)
	diez  = decimal.NewFromInt(10)
	cinco = decimal.NewFromInt(5)
	cero  = decimal.Zero
)

// montoScale devuelve la cantidad de decimales a usar según la moneda.
// PYG no admite decimales; el resto de monedas admiten hasta 4 (Manual F).
func montoScale(moneda string) int32 {
	if moneda == "" || strings.EqualFold(moneda, "PYG") {
		return 0
	}
	return 4
}

// roundMonto redondea un monto al número de decimales válido para la moneda.
func roundMonto(v decimal.Decimal, moneda string) decimal.Decimal {
	return v.Round(montoScale(moneda))
}

// effectiveTotOpe devuelve el total de la operación del ítem (dTotOpeItem si
// hay gValorRestaItem, o el bruto en caso contrario).
func effectiveTotOpe(it GCamItem) decimal.Decimal {
	if it.GValorItem == nil {
		return cero
	}
	if it.GValorItem.GValorRestaItem != nil {
		return it.GValorItem.GValorRestaItem.DTotOpeItem
	}
	return it.GValorItem.DTotBruOpeItem
}

// CalcularTotales agrega los importes de todos los ítems en el subtotal general
// (gTotSub), separando por afectación de IVA y por tasa (5 % / 10 % / exenta /
// exonerada). No aplica descuentos ni anticipos globales (quedan en cero).
//
// Reglas (Manual Técnico, campos F):
//   - dSub5/dSub10  = Σ EA008 (dTotOpeItem completo) de los ítems a esa tasa
//     (E731 = 1 o 4). En gravado parcial se suma el total del ítem, no solo la
//     parte gravada.
//   - dBaseGrav5/10 = Σ base gravada a esa tasa.
//   - dIVA5/dIVA10  = Σ IVA liquidado a esa tasa.
//   - dSubExe       = Σ EA008 de los ítems exentos (E731 = 3).
//   - dSubExo       = Σ EA008 de los ítems exonerados (E731 = 2).
//   - dTotOpe       = Σ EA008 de cada ítem = F002 + F003 + F004 + F005.
// calcularTotalesAutofactura arma el gTotSub de una autofactura (C002=4): sin IVA
// (los ítems no llevan gCamIVA), dTotOpe = dTotGralOpe = Σ dTotOpeItem y todos los
// subtotales de IVA en cero.
func calcularTotalesAutofactura(items []GCamItem, moneda string) GTotSub {
	total := cero
	for _, it := range items {
		total = total.Add(effectiveTotOpe(it))
	}
	total = roundMonto(total, moneda)
	return GTotSub{
		DSubExe: cero, DSubExo: cero, DSub5: cero, DSub10: cero,
		DTotOpe: total, DTotDesc: cero, DTotDescGlotem: cero,
		DTotAntItem: cero, DTotAnt: cero, DPorcDescTotal: cero,
		DDescTotal: cero, DAnticipo: cero, DRedon: cero, DTotGralOpe: total,
		DTotIVA5: cero, DTotIVA10: cero, DTotIVA: cero,
		DBaseGrav5: cero, DBaseGrav10: cero, DTBasGraIVA: cero,
	}
}

func CalcularTotales(items []GCamItem, moneda string) GTotSub {
	t := GTotSub{
		DSubExe: cero, DSubExo: cero, DSub5: cero, DSub10: cero,
		DTotOpe: cero, DTotDesc: cero, DTotDescGlotem: cero,
		DTotAntItem: cero, DTotAnt: cero, DPorcDescTotal: cero,
		DDescTotal: cero, DAnticipo: cero, DRedon: cero, DTotGralOpe: cero,
		DTotIVA5: cero, DTotIVA10: cero, DTotIVA: cero,
		DBaseGrav5: cero, DBaseGrav10: cero, DTBasGraIVA: cero,
	}

	for _, it := range items {
		if it.GCamIVA == nil {
			continue
		}
		iva := it.GCamIVA
		ope := effectiveTotOpe(it)
		t.DTotOpe = t.DTotOpe.Add(ope)

		switch iva.IAfecIVA {
		case AfecIVAExonerado:
			t.DSubExo = t.DSubExo.Add(ope)
		case AfecIVAExento:
			t.DSubExe = t.DSubExe.Add(ope)
		case AfecIVAGravado, AfecIVAParcial:
			// dSub5/dSub10 usan el EA008 completo del ítem (incluye la parte
			// exenta en gravado parcial); dBaseGrav/dIVA usan base y liquidación.
			switch {
			case iva.DTasaIVA.Equal(diez):
				t.DSub10 = t.DSub10.Add(ope)
				t.DBaseGrav10 = t.DBaseGrav10.Add(iva.DBasGravIVA)
				t.DTotIVA10 = t.DTotIVA10.Add(iva.DLiqIVAItem)
			case iva.DTasaIVA.Equal(cinco):
				t.DSub5 = t.DSub5.Add(ope)
				t.DBaseGrav5 = t.DBaseGrav5.Add(iva.DBasGravIVA)
				t.DTotIVA5 = t.DTotIVA5.Add(iva.DLiqIVAItem)
			}
		}
	}

	t.DTotIVA = t.DTotIVA5.Add(t.DTotIVA10)
	t.DTBasGraIVA = t.DBaseGrav5.Add(t.DBaseGrav10)
	// Sin descuentos/anticipos globales: dTotGralOpe = dTotOpe + dRedon.
	t.DTotGralOpe = t.DTotOpe.
		Sub(t.DDescTotal).
		Sub(t.DAnticipo).
		Add(t.DRedon)
	return t
}
