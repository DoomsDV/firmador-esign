package sifen

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

const (
	QRBaseURLTest = "https://ekuatia.set.gov.py/consultas-test/qr?"
)

// QRParams parámetros para armar dCarQR (MT 13.8).
type QRParams struct {
	CDC         string
	FeEmiDE     string // dFeEmiDE crudo, ej. 2017-01-25T09:35:17
	RecField    string // "dRucRec" (contribuyente) o "dNumIDRec" (no contribuyente); vacío → "dRucRec"
	RecID       string // dRucRec o dNumIDRec; vacío → "0"
	TotGralOpe  string
	TotIVA      string
	Items       int
	DigestValue string // Base64 del DigestValue de la firma
	IdCSC       string
	CSC         string
	QRBase      string      // vacío → base oficial del ambiente (Env)
	Env         Environment // "" → EnvTest (compatibilidad con los CLIs)
}

// BuildQueryString arma el query del Paso 1 (sin CSC, sin cHashQR).
func BuildQueryString(p QRParams) string {
	rec := p.RecID
	if rec == "" {
		rec = "0"
	}
	tot := p.TotGralOpe
	if tot == "" {
		tot = "0"
	}
	iva := p.TotIVA
	if iva == "" {
		iva = "0"
	}
	idCSC := p.IdCSC
	if idCSC == "" {
		idCSC = "0001"
	}
	recField := p.RecField
	if recField == "" {
		recField = "dRucRec"
	}

	return fmt.Sprintf(
		"nVersion=%d&Id=%s&dFeEmiDE=%s&%s=%s&dTotGralOpe=%s&dTotIVA=%s&cItems=%d&DigestValue=%s&IdCSC=%s",
		Version,
		p.CDC,
		toHexASCII(p.FeEmiDE),
		recField,
		rec,
		tot,
		iva,
		p.Items,
		toHexASCII(p.DigestValue),
		idCSC,
	)
}

// HashQR calcula cHashQR = SHA-256(query + CSC) en hex minúscula.
func HashQR(query, csc string) string {
	sum := sha256.Sum256([]byte(query + csc))
	return hex.EncodeToString(sum[:])
}

// BuildCarQR genera la URL completa del QR solo en ambiente de prueba (consultas-test).
func BuildCarQR(p QRParams) (string, error) {
	if p.CDC == "" || p.FeEmiDE == "" || p.DigestValue == "" {
		return "", fmt.Errorf("faltan CDC, FeEmiDE o DigestValue para el QR")
	}
	if p.CSC == "" {
		return "", fmt.Errorf("falta CSC para el QR")
	}

	env := p.Env
	if env == "" {
		env = EnvTest
	}
	base := p.QRBase
	if base == "" {
		base = EndpointsFor(env).QRBase
	}
	if err := AssertSafeURL(base, env); err != nil {
		return "", err
	}

	query := BuildQueryString(p)
	hash := HashQR(query, p.CSC)
	return base + query + "&cHashQR=" + hash, nil
}

func toHexASCII(s string) string {
	return hex.EncodeToString([]byte(s))
}

// ReceptorIDForQR elige dRucRec o dNumIDRec; si no hay, "0".
func ReceptorIDForQR(rec GDatRec) string {
	if rec.DRucRec != "" {
		return rec.DRucRec
	}
	if rec.DNumIDRec != "" {
		return rec.DNumIDRec
	}
	return "0"
}

// ReceptorFieldForQR devuelve el nombre del parámetro QR según el tipo de receptor:
// "dRucRec" si es contribuyente (tiene RUC), "dNumIDRec" en caso contrario (MT 13.8).
func ReceptorFieldForQR(rec GDatRec) string {
	if rec.DRucRec != "" {
		return "dRucRec"
	}
	return "dNumIDRec"
}

// DecimalPlain formatea decimal sin notación científica (para QR/totales).
func DecimalPlain(v interface{ String() string }) string {
	s := v.String()
	// shopspring puede devolver "150000" o "150000.0"
	if i, err := strconv.ParseFloat(s, 64); err == nil && i == float64(int64(i)) {
		return strconv.FormatInt(int64(i), 10)
	}
	return s
}
