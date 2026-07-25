package sifen

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"time"

	"github.com/beevik/etree"
)

// Tipos de evento del emisor (Manual GDE006 dTiGDE).
const (
	TiEventoCancelacion   = 1
	TiEventoInutilizacion = 2
)

// REve es el grupo firmado de un evento (GDE002). Su atributo Id se referencia
// desde la firma (URI="#Id"). El esquema siRecepEvento_v150 solo define
// dFecFirma, dVerFor y gGroupTiEvt dentro de rEve (no lleva dTiGDE).
type REve struct {
	XMLName     xml.Name    `xml:"rEve"`
	Id          string      `xml:"Id,attr"`
	DFecFirma   string      `xml:"dFecFirma"`
	DVerFor     int         `xml:"dVerFor"`
	GGroupTiEvt GGroupTiEvt `xml:"gGroupTiEvt"`
}

// GGroupTiEvt contiene el grupo específico según el tipo de evento (GDE007).
type GGroupTiEvt struct {
	RGeVeCan *RGeVeCan `xml:"rGeVeCan,omitempty"`
	RGeVeInu *RGeVeInu `xml:"rGeVeInu,omitempty"`
}

// RGeVeCan es el evento de cancelación de un DTE (GEC001).
type RGeVeCan struct {
	Id     string `xml:"Id"`     // CDC del DTE a cancelar (44 dígitos)
	MOtEve string `xml:"mOtEve"` // motivo (5-500 caracteres)
}

// RGeVeInu es el evento de inutilización de un rango de numeración (GEI001).
type RGeVeInu struct {
	DNumTim string `xml:"dNumTim"` // timbrado (8 dígitos)
	DEst    string `xml:"dEst"`    // establecimiento (3)
	DPunExp string `xml:"dPunExp"` // punto de expedición (3)
	DNumIn  string `xml:"dNumIn"`  // número inicial del rango (7)
	DNumFin string `xml:"dNumFin"` // número final del rango (7)
	ITiDE   int    `xml:"iTiDE"`   // tipo de DE
	MOtEve  string `xml:"mOtEve"`  // motivo (5-500)
}

// NewEventoCancelacion arma el rEve de una cancelación (dTiGDE=1).
func NewEventoCancelacion(idEvento int, cdc, motivo string, fecha time.Time) (*REve, error) {
	if len(cdc) != 44 {
		return nil, fmt.Errorf("CDC a cancelar inválido (esperado 44 dígitos): %q", cdc)
	}
	if l := len([]rune(motivo)); l < 5 || l > 500 {
		return nil, fmt.Errorf("motivo del evento debe tener 5-500 caracteres, got %d", l)
	}
	if idEvento <= 0 {
		return nil, fmt.Errorf("id de evento inválido: %d", idEvento)
	}
	return &REve{
		Id:        strconv.Itoa(idEvento),
		DFecFirma: fecha.Format("2006-01-02T15:04:05"),
		DVerFor:   Version,
		GGroupTiEvt: GGroupTiEvt{
			RGeVeCan: &RGeVeCan{Id: cdc, MOtEve: motivo},
		},
	}, nil
}

// NewEventoInutilizacion arma el rEve de una inutilización de rango (dTiGDE=2).
func NewEventoInutilizacion(idEvento int, timbrado, est, punExp string, numIn, numFin, tiDE int, motivo string, fecha time.Time) (*REve, error) {
	if idEvento <= 0 {
		return nil, fmt.Errorf("id de evento inválido: %d", idEvento)
	}
	if l := len([]rune(motivo)); l < 5 || l > 500 {
		return nil, fmt.Errorf("motivo del evento debe tener 5-500 caracteres, got %d", l)
	}
	if numIn <= 0 || numFin < numIn {
		return nil, fmt.Errorf("rango inválido: [%d, %d]", numIn, numFin)
	}
	if numFin-numIn+1 > 1000 {
		return nil, fmt.Errorf("la inutilización admite hasta 1000 números, got %d", numFin-numIn+1)
	}
	if _, ok := tipoDEDesc[tiDE]; !ok {
		return nil, fmt.Errorf("iTiDE inválido para inutilización: %d", tiDE)
	}
	return &REve{
		Id:        strconv.Itoa(idEvento),
		DFecFirma: fecha.Format("2006-01-02T15:04:05"),
		DVerFor:   Version,
		GGroupTiEvt: GGroupTiEvt{
			RGeVeInu: &RGeVeInu{
				DNumTim: timbrado,
				DEst:    est,
				DPunExp: punExp,
				DNumIn:  fmt.Sprintf("%07d", numIn),
				DNumFin: fmt.Sprintf("%07d", numFin),
				ITiDE:   tiDE,
				MOtEve:  motivo,
			},
		},
	}, nil
}

// FirmarEvento firma el rEve y devuelve el XML del grupo de eventos ya firmado
// (<gGroupGesEve><rGesEve><rEve.../><Signature.../></rGesEve></gGroupGesEve>),
// listo para insertarse en el dEvReg del envelope siRecepEvento.
func FirmarEvento(ev *REve, cert *DigitalCertificate) ([]byte, error) {
	if ev == nil {
		return nil, fmt.Errorf("evento nulo")
	}
	if ev.Id == "" {
		return nil, fmt.Errorf("el evento no tiene Id")
	}
	if cert == nil || cert.PrivateKey == nil || cert.Certificate == nil {
		return nil, fmt.Errorf("certificado inválido")
	}

	raw, err := xml.Marshal(ev)
	if err != nil {
		return nil, fmt.Errorf("marshal rEve: %w", err)
	}

	doc := etree.NewDocument()
	doc.WriteSettings.CanonicalText = true
	if err := doc.ReadFromBytes(raw); err != nil {
		return nil, fmt.Errorf("parse rEve: %w", err)
	}
	reveEl := doc.Root()
	if reveEl == nil {
		return nil, fmt.Errorf("rEve raíz nula")
	}

	sigEl, _, err := signEnvelopedElement(reveEl, ev.Id, cert)
	if err != nil {
		return nil, err
	}

	// Ensamblado: gGroupGesEve > rGesEve > [rEve, Signature].
	out := etree.NewDocument()
	out.WriteSettings.CanonicalText = true
	group := out.CreateElement("gGroupGesEve")
	group.CreateAttr("xmlns:xsi", "http://www.w3.org/2001/XMLSchema-instance")
	group.CreateAttr("xsi:schemaLocation", SifenNS+" siRecepEvento_v150.xsd")
	gesEve := group.CreateElement("rGesEve")
	gesEve.AddChild(reveEl.Copy())
	gesEve.AddChild(sigEl)

	xmlBytes, err := out.WriteToBytes()
	if err != nil {
		return nil, fmt.Errorf("serializar evento: %w", err)
	}
	return xmlBytes, nil
}
