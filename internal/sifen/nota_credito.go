package sifen

import "fmt"

// motivoEmisionDescs mapea iMotEmi a su descripción oficial (Manual E401).
var motivoEmisionDescs = map[int]string{
	1: "Devolución y Ajuste de precios",
	2: "Devolución",
	3: "Descuento",
	4: "Bonificación",
	5: "Crédito incobrable",
	6: "Recupero de costo",
	7: "Recupero de gasto",
	8: "Ajuste de precio",
}

// motivoEmisionDesc devuelve la descripción del motivo de emisión de NCE/NDE.
func motivoEmisionDesc(motivo int) (string, error) {
	d, ok := motivoEmisionDescs[motivo]
	if !ok {
		return "", fmt.Errorf("iMotEmi inválido: %d", motivo)
	}
	return d, nil
}

// NewDEAsocElectronico arma un documento asociado que referencia un DE
// electrónico existente por su CDC (iTipDocAso=1). Es el caso típico de una NCE
// que ajusta una factura electrónica ya aprobada.
func NewDEAsocElectronico(cdcRef string) (GCamDEAsoc, error) {
	if len(cdcRef) != 44 {
		return GCamDEAsoc{}, fmt.Errorf("CDC referenciado inválido (esperado 44 dígitos): %q", cdcRef)
	}
	return GCamDEAsoc{
		ITipDocAso:    1,
		DDesTipDocAso: "Electrónico",
		DCdCDERef:     cdcRef,
	}, nil
}

// tiposConstanciaDescs mapea iTipCons a su descripción (Manual H014/H015).
var tiposConstanciaDescs = map[int]string{
	1: "Constancia de no ser contribuyente",
	2: "Constancia de microproductores",
}

// NewDEAsocConstancia arma un documento asociado del tipo Constancia Electrónica
// (iTipDocAso=3), usado por la autofactura para acreditar que el vendedor no es
// contribuyente. tipoCons: 1 = no contribuyente, 2 = microproductores. numCons y
// numControl solo aplican a microproductores (H002=3 y H014=2).
func NewDEAsocConstancia(tipoCons, numCons int, numControl string) (GCamDEAsoc, error) {
	desc, ok := tiposConstanciaDescs[tipoCons]
	if !ok {
		return GCamDEAsoc{}, fmt.Errorf("iTipCons inválido: %d", tipoCons)
	}
	asoc := GCamDEAsoc{
		ITipDocAso:    3,
		DDesTipDocAso: "Constancia Electrónica",
		ITipCons:      tipoCons,
		DDesTipCons:   desc,
	}
	if tipoCons == 2 {
		if numCons <= 0 {
			return GCamDEAsoc{}, fmt.Errorf("constancia de microproductores requiere dNumCons > 0")
		}
		asoc.DNumCons = numCons
		asoc.DNumControl = numControl
	}
	return asoc, nil
}
