package sifen

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// GenerateCDC construye el Código de Control (44 caracteres)
func GenerateCDC(
	tipoDocumento int, // 01=Factura
	rucEmisor string, // RUC sin DV
	dvRucEmisor int, // DV del RUC
	codEstablecimiento int, // 001
	puntoExpedicion int, // 001
	numeroDocumento int, // Número de factura
	tipoContribuyente int, // 1=Física, 2=Jurídica
	fechaEmision time.Time, // Fecha
	tipoEmision int, // 1=Normal, 2=Contingencia
	codigoSeguridad int, // Aleatorio
) string {

	// 1. Formateo
	pTipo := fmt.Sprintf("%02d", tipoDocumento)

	pRuc := strings.TrimSpace(rucEmisor)
	if len(pRuc) > 8 {
		pRuc = pRuc[:8]
	}
	// RUC debe tener padding de ceros a la izquierda hasta 8 dígitos
	pRuc = strings.TrimSpace(rucEmisor)
	if len(pRuc) > 8 {
		pRuc = pRuc[:8]
	}
	// Rellenar con ceros a la izquierda manualmente
	for len(pRuc) < 8 {
		pRuc = "0" + pRuc
	}

	pDvRuc := fmt.Sprintf("%d", dvRucEmisor)
	pEst := fmt.Sprintf("%03d", codEstablecimiento)
	pPun := fmt.Sprintf("%03d", puntoExpedicion)
	pNum := fmt.Sprintf("%07d", numeroDocumento)
	pTipoCont := fmt.Sprintf("%d", tipoContribuyente)
	pFecha := fechaEmision.Format("20060102")
	pTipEmi := fmt.Sprintf("%d", tipoEmision)
	pCodSeg := fmt.Sprintf("%09d", codigoSeguridad)

	// 2. Concatenación
	cdcRaw := pTipo + pRuc + pDvRuc + pEst + pPun + pNum + pTipoCont + pFecha + pTipEmi + pCodSeg

	// 3. Calculamos DV
	dvCDC := CalculateModulo11(cdcRaw)

	// 4. Retorno final
	return cdcRaw + strconv.Itoa(dvCDC)
}

// CalculateModulo11 realiza el cálculo matemático ponderado
func CalculateModulo11(input string) int {
	total := 0
	factor := 2

	for i := len(input) - 1; i >= 0; i-- {
		digit, err := strconv.Atoi(string(input[i]))
		if err != nil {
			return 0
		}
		total += digit * factor
		factor++
		if factor > 11 {
			factor = 2
		}
	}

	remainder := total % 11
	result := 11 - remainder

	if result > 9 {
		result = 0
	}
	return result
}

// GenerateSecurityCode crea un número aleatorio de 9 dígitos.
func GenerateSecurityCode() int {
	max := big.NewInt(999999999)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return 123456789 // Fallback seguro
	}
	return int(n.Int64()) + 1
}
