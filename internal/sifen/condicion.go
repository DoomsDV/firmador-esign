package sifen

import (
	"fmt"

	"github.com/shopspring/decimal"
)

// CondicionContado arma gCamCond con iCondOpe=1 y pagos iniciales (sin gPagCred).
func CondicionContado(pagos ...GPaConEIni) (*GCamCond, error) {
	if len(pagos) == 0 {
		return nil, fmt.Errorf("contado requiere al menos un gPaConEIni")
	}
	return &GCamCond{
		ICondOpe:   1,
		DDCondOpe:  "Contado",
		GPaConEIni: pagos,
		GPagCred:   nil,
	}, nil
}

// CondicionCreditoPlazo arma crédito con iCondCred=1 (Plazo).
// entregaInicial y pagoInicial son opcionales; si hay entrega, ambos deben informarse.
func CondicionCreditoPlazo(plazo string, entregaInicial *decimal.Decimal, pagoInicial *GPaConEIni) (*GCamCond, error) {
	if plazo == "" {
		return nil, fmt.Errorf("crédito a plazo requiere dPlazoCre")
	}
	cred := &GPagCred{
		ICondCred:  1,
		DDCondCred: "Plazo",
		DPlazoCre:  plazo,
	}
	cond := &GCamCond{
		ICondOpe:  2,
		DDCondOpe: "Crédito",
		GPagCred:  cred,
	}
	if entregaInicial != nil {
		if pagoInicial == nil {
			return nil, fmt.Errorf("entrega inicial requiere gPaConEIni")
		}
		cred.DMonEnt = entregaInicial
		cond.GPaConEIni = []GPaConEIni{*pagoInicial}
	}
	return cond, nil
}

// CondicionCreditoCuotas arma crédito con iCondCred=2 (Cuota).
func CondicionCreditoCuotas(nCuotas int, cuotas []GCuota, entregaInicial *decimal.Decimal, pagoInicial *GPaConEIni) (*GCamCond, error) {
	if nCuotas <= 0 {
		return nil, fmt.Errorf("crédito en cuotas requiere dCuotas > 0")
	}
	if len(cuotas) == 0 {
		return nil, fmt.Errorf("crédito en cuotas requiere gCuotas")
	}
	cred := &GPagCred{
		ICondCred:  2,
		DDCondCred: "Cuota",
		DCuotas:    nCuotas,
		GCuotas:    cuotas,
	}
	cond := &GCamCond{
		ICondOpe:  2,
		DDCondOpe: "Crédito",
		GPagCred:  cred,
	}
	if entregaInicial != nil {
		if pagoInicial == nil {
			return nil, fmt.Errorf("entrega inicial requiere gPaConEIni")
		}
		cred.DMonEnt = entregaInicial
		cond.GPaConEIni = []GPaConEIni{*pagoInicial}
	}
	return cond, nil
}

// PagoTarjetaCreditoPYG arma gPaConEIni con iTiPago=3 (Tarjeta de crédito)
// e incluye gPagTarCD (denominación genérica / POS) exigido en homologación.
func PagoTarjetaCreditoPYG(monto decimal.Decimal) GPaConEIni {
	return GPaConEIni{
		ITiPago:     3,
		DDesTiPag:   "Tarjeta de crédito",
		DMonTiPag:   monto,
		CMoneTiPag:  "PYG",
		DDMoneTiPag: "Guarani",
		GPagTarCD: &GPagTarCD{
			IDenTarj:    99, // Otra
			DDesDenTarj: "Otra",
			IForProPa:   1, // POS
		},
	}
}

// CatalogoDesTiPago devuelve la descripción oficial del catálogo SIFEN para iTiPago.
// String vacío = código desconocido / sin descripción válida.
func CatalogoDesTiPago(iTiPago int) string {
	switch iTiPago {
	case 1:
		return "Efectivo"
	case 2:
		return "Cheque"
	case 3:
		return "Tarjeta de crédito"
	case 4:
		return "Tarjeta de débito"
	case 5:
		return "Transferencia"
	case 6:
		return "Giro"
	case 7:
		return "Billetera electrónica"
	case 8:
		return "Tarjeta empresarial"
	case 9:
		return "Vale"
	case 10:
		return "Retención"
	case 11:
		return "Pago por anticipo"
	case 12:
		return "Valor fiscal"
	case 13:
		return "Valor comercial"
	case 14:
		return "Compensación"
	case 15:
		return "Permuta"
	case 16:
		return "Pago bancario"
	case 17:
		return "Pago móvil"
	case 18:
		return "Donación"
	case 19:
		return "Promoción"
	case 20:
		return "Consumo interno"
	case 21:
		return "Pago electrónico"
	case 99:
		return "Otro"
	default:
		return ""
	}
}

// PagoEfectivoPYG ayuda rápida para gPaConEIni en contado/entrega.
func PagoEfectivoPYG(monto decimal.Decimal) GPaConEIni {
	return GPaConEIni{
		ITiPago:     1,
		DDesTiPag:   "Efectivo",
		DMonTiPag:   monto,
		CMoneTiPag:  "PYG",
		DDMoneTiPag: "Guarani",
	}
}
