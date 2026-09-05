package webhook

import "time"

// EventPayload es el cuerpo JSON de invoice.ready enviado al receptor.
type EventPayload struct {
	EventID         string `json:"event_id"`
	Event           string `json:"event"`
	OccurredAt      string `json:"occurred_at"`
	Environment     string `json:"environment"`
	CDC             string `json:"cdc"`
	IdempotencyKey  string `json:"idempotency_key,omitempty"`
	Estado          string `json:"estado"`
	ProtAut         string `json:"prot_aut,omitempty"`
	KudeURL         string `json:"kude_url"`
	XMLSHA256       string `json:"xml_sha256"`
	XMLSize         int    `json:"xml_size"`
	XMLMime         string `json:"xml_mime,omitempty"`
	XMLURL          string `json:"xml_url"`
}

// Config del worker de entregas webhook.
type Config struct {
	Interval     time.Duration
	Batch        int
	LeaseSeconds int
	Enabled      bool
	Timeout      time.Duration
	PublicAPIURL string // base pública del firmador, ej. https://api-staging.etick.uno
}
