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
