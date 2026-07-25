package sifen

import (
	"fmt"
	"strings"
)

// ReceptorGeo datos de ubicación del receptor (opcionales según caso).
type ReceptorGeo struct {
	Dir        string
	NumCas     string
	CDep       int
	DDesDep    string
	CDis       int
	DDesDis    string
	CCiu       int
	DDesCiu    string
	Tel        string
	Email      string
	CodCliente string
}

// Tipos de identificación del receptor no contribuyente (Manual D206 iTipIDRec).
const (
	TipIDCedulaPY    = 1 // Cédula paraguaya
	TipIDPasaporte   = 2 // Pasaporte
	TipIDCedulaExt   = 3 // Cédula extranjera
	TipIDCarnetResid = 4 // Carnet de residencia
	TipIDInnominado  = 5 // Innominado
	TipIDTarjDiplo   = 6 // Tarjeta Diplomática de exoneración fiscal
	TipIDOtro        = 9 // Otro
)

// tipIDRecDescs mapea iTipIDRec a su descripción oficial (dDTipIDRec).
var tipIDRecDescs = map[int]string{
	TipIDCedulaPY:    "Cédula paraguaya",
	TipIDPasaporte:   "Pasaporte",
	TipIDCedulaExt:   "Cédula extranjera",
	TipIDCarnetResid: "Carnet de residencia",
	TipIDInnominado:  "Innominado",
	TipIDTarjDiplo:   "Tarjeta Diplomática de exoneración fiscal",
	TipIDOtro:        "Otro",
}

// tipIDRecDesc devuelve la descripción del tipo de identificación del receptor.
func tipIDRecDesc(t int) (string, error) {
	d, ok := tipIDRecDescs[t]
	if !ok {
		return "", fmt.Errorf("iTipIDRec inválido: %d", t)
	}
	return d, nil
}

// NewReceptorNoContribuyente arma un receptor no contribuyente nacional (B2C)
// con cualquier tipo de identificación válido (iTipIDRec genérico).
func NewReceptorNoContribuyente(tipoID int, numID, nombre string, geo ReceptorGeo) (GDatRec, error) {
	numID = strings.TrimSpace(numID)
	nombre = strings.TrimSpace(nombre)
	if numID == "" {
		return GDatRec{}, fmt.Errorf("número de identificación del receptor vacío")
	}
	if len(nombre) < 4 {
		return GDatRec{}, fmt.Errorf("nombre del receptor inválido")
	}
	desID, err := tipIDRecDesc(tipoID)
	if err != nil {
		return GDatRec{}, err
	}

	rec := GDatRec{
		INatRec:     2, // no contribuyente
		ITiOpe:      2, // B2C
		CPaisRec:    "PRY",
		DDesPaisRe:  "Paraguay",
		ITipIDRec:   tipoID,
		DDTipIDRec:  desID,
		DNumIDRec:   numID,
		DNomRec:     nombre,
		DDirRec:     geo.Dir,
		DNumCasRec:  geo.NumCas,
		CDepRec:     geo.CDep,
		DDesDepRec:  geo.DDesDep,
		CDisRec:     geo.CDis,
		DDesDisRec:  geo.DDesDis,
		CCiuRec:     geo.CCiu,
		DDesCiuRec:  geo.DDesCiu,
		DTelRec:     geo.Tel,
		DEmailRec:   geo.Email,
		DCodCliente: geo.CodCliente,
	}
	if err := ValidateReceptor(rec); err != nil {
		return GDatRec{}, err
	}
	return rec, nil
}

// NewReceptorCI arma un receptor no contribuyente identificado con cédula PY (D201=2).
func NewReceptorCI(ci, nombre string, geo ReceptorGeo) (GDatRec, error) {
	return NewReceptorNoContribuyente(TipIDCedulaPY, ci, nombre, geo)
}

// NewReceptorInnominado arma un receptor sin identificar (venta a consumidor final
// sin datos): iNatRec=2, iTipIDRec=5, dNumIDRec="0", dNomRec="Sin Nombre".
func NewReceptorInnominado() (GDatRec, error) {
	rec := GDatRec{
		INatRec:    2,
		ITiOpe:     2, // B2C
		CPaisRec:   "PRY",
		DDesPaisRe: "Paraguay",
		ITipIDRec:  TipIDInnominado,
		DDTipIDRec: tipIDRecDescs[TipIDInnominado],
		DNumIDRec:  "0",
		DNomRec:    "Sin Nombre",
	}
	if err := ValidateReceptor(rec); err != nil {
		return GDatRec{}, err
	}
	return rec, nil
}

// NewReceptorExtranjero arma un receptor del exterior (operación B2F, iTiOpe=4).
// pais es el código ISO de 3 letras (≠ PRY) y desPais su descripción.
func NewReceptorExtranjero(pais, desPais string, tipoID int, numID, nombre string, geo ReceptorGeo) (GDatRec, error) {
	pais = strings.ToUpper(strings.TrimSpace(pais))
	nombre = strings.TrimSpace(nombre)
	numID = strings.TrimSpace(numID)
	if pais == "" || pais == "PRY" {
		return GDatRec{}, fmt.Errorf("receptor extranjero requiere cPaisRec ≠ PRY, got %q", pais)
	}
	if len(nombre) < 4 {
		return GDatRec{}, fmt.Errorf("nombre del receptor inválido")
	}
	desID, err := tipIDRecDesc(tipoID)
	if err != nil {
		return GDatRec{}, err
	}

	rec := GDatRec{
		INatRec:     2,
		ITiOpe:      4, // B2F (exterior)
		CPaisRec:    pais,
		DDesPaisRe:  desPais,
		ITipIDRec:   tipoID,
		DDTipIDRec:  desID,
		DNumIDRec:   numID,
		DNomRec:     nombre,
		DDirRec:     geo.Dir,
		DNumCasRec:  geo.NumCas,
		DTelRec:     geo.Tel,
		DEmailRec:   geo.Email,
		DCodCliente: geo.CodCliente,
	}
	if err := ValidateReceptor(rec); err != nil {
		return GDatRec{}, err
	}
	return rec, nil
}

// NewReceptorRUC arma un receptor contribuyente con RUC (DNIT D201=1).
// tipCont: 1=Persona Física, 2=Persona Jurídica. iTiOpe típico B2B=1.
func NewReceptorRUC(ruc string, dv, tipCont int, nombre string, iTiOpe int, geo ReceptorGeo) (GDatRec, error) {
	ruc = strings.TrimSpace(ruc)
	nombre = strings.TrimSpace(nombre)
	if ruc == "" {
		return GDatRec{}, fmt.Errorf("RUC del receptor vacío")
	}
	if tipCont != 1 && tipCont != 2 {
		return GDatRec{}, fmt.Errorf("tipo de contribuyente receptor inválido: %d", tipCont)
	}
	if iTiOpe == 0 {
		iTiOpe = 1 // B2B por defecto
	}
	if len(nombre) < 4 {
		return GDatRec{}, fmt.Errorf("nombre del receptor inválido")
	}

	rec := GDatRec{
		INatRec:     1, // contribuyente
		ITiOpe:      iTiOpe,
		CPaisRec:    "PRY",
		DDesPaisRe:  "Paraguay",
		ITiContRec:  tipCont,
		DRucRec:     ruc,
		DDVRec:      dv,
		DNomRec:     nombre,
		DDirRec:     geo.Dir,
		DNumCasRec:  geo.NumCas,
		CDepRec:     geo.CDep,
		DDesDepRec:  geo.DDesDep,
		CDisRec:     geo.CDis,
		DDesDisRec:  geo.DDesDis,
		CCiuRec:     geo.CCiu,
		DDesCiuRec:  geo.DDesCiu,
		DTelRec:     geo.Tel,
		DEmailRec:   geo.Email,
		DCodCliente: geo.CodCliente,
	}
	if err := ValidateReceptor(rec); err != nil {
		return GDatRec{}, err
	}
	return rec, nil
}

// ValidateReceptor aplica reglas DNIT básicas de combinación naturaleza / ID.
func ValidateReceptor(rec GDatRec) error {
	switch rec.INatRec {
	case 1: // contribuyente
		if rec.DRucRec == "" {
			return fmt.Errorf("contribuyente requiere dRucRec")
		}
		if rec.ITiContRec == 0 {
			return fmt.Errorf("contribuyente requiere iTiContRec")
		}
		if rec.ITipIDRec != 0 || rec.DNumIDRec != "" {
			return fmt.Errorf("contribuyente no debe informar iTipIDRec/dNumIDRec")
		}
	case 2: // no contribuyente
		if rec.ITiOpe != 2 && rec.ITiOpe != 4 {
			return fmt.Errorf("no contribuyente requiere iTiOpe B2C(2) o B2F(4), got %d", rec.ITiOpe)
		}
		if rec.DRucRec != "" || rec.ITiContRec != 0 {
			return fmt.Errorf("no contribuyente no debe informar RUC/iTiContRec")
		}
		if rec.ITiOpe != 4 && (rec.ITipIDRec == 0 || rec.DNumIDRec == "") {
			return fmt.Errorf("no contribuyente B2C requiere iTipIDRec y dNumIDRec")
		}
	default:
		return fmt.Errorf("iNatRec inválido: %d", rec.INatRec)
	}
	return nil
}
