package sifen

import (
	"encoding/xml"
	"fmt"
)

// ParseRDEXML deserializa un rDE firmado (bytes canónicos) a *RDE.
// Solo para reconstrucción de KuDE / lectura; NUNCA re-firmar ni reenviar el
// resultado marshalizado (regla 0141: el XML firmado es inmutable).
func ParseRDEXML(xmlFirmado []byte) (*RDE, error) {
	if len(xmlFirmado) == 0 {
		return nil, fmt.Errorf("xml firmado vacío")
	}
	var rde RDE
	if err := xml.Unmarshal(xmlFirmado, &rde); err != nil {
		return nil, fmt.Errorf("parsear rDE: %w", err)
	}
	if rde.DE == nil {
		return nil, fmt.Errorf("rDE sin elemento DE")
	}
	return &rde, nil
}
