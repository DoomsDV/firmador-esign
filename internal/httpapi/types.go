package httpapi

import "github.com/shopspring/decimal"

// createDocumentRequest es el payload de POST /v1/documents. Los montos usan
// decimal.Decimal (acepta número o string en JSON sin perder precisión).
type createDocumentRequest struct {
	Tipo           string          `json:"tipo"`      // fe | nce | nde
	Condicion      string          `json:"condicion"` // contado | credito
	Plazo          string          `json:"plazo"`     // días (crédito a plazo); default "30"
	DatosOperacion datosOperacion  `json:"datos_operacion"`
	Receptor       receptorRequest `json:"receptor"`
	Moneda         string          `json:"moneda"`
	TipoCambio     *decimal.Decimal `json:"tipoCambio"`
	Items          []itemRequest   `json:"items"`

	// NCE / NDE
	CdcRef string `json:"cdcRef"`
	Motivo int    `json:"motivo"`
}

type datosOperacion struct {
	Establecimiento string `json:"establecimiento"`
	PuntoExpedicion string `json:"punto_expedicion"`
}

type receptorRequest struct {
	Tipo               string      `json:"tipo"` // ci | ruc | innominado | extranjero
	Documento          string      `json:"documento"`
	DV                 int         `json:"dv"`                 // RUC receptor
	TipoContribuyente  int         `json:"tipoContribuyente"`  // 1 física, 2 jurídica (RUC)
	TipoOperacion      int         `json:"tipoOperacion"`      // iTiOpe (RUC): 1 B2B, 2 B2C...
	Nombre             string      `json:"nombre"`
	Pais               string      `json:"pais"`               // extranjero (ISO-3 ≠ PRY)
	DesPais            string      `json:"desPais"`
	TipoIdentificacion int         `json:"tipoIdentificacion"` // extranjero (iTipIDRec)
	Geo                *geoRequest `json:"geo"`
}

type geoRequest struct {
	Dir     string `json:"dir"`
	NumCas  string `json:"numCas"`
	DepCod  int    `json:"depCod"`
	DepDesc string `json:"depDesc"`
	DisCod  int    `json:"disCod"`
	DisDesc string `json:"disDesc"`
	CiuCod  int    `json:"ciuCod"`
	CiuDesc string `json:"ciuDesc"`
	Tel     string `json:"tel"`
	Email   string `json:"email"`
}

type itemRequest struct {
	Codigo          string          `json:"codigo"`
	Descripcion     string          `json:"descripcion"`
	Cantidad        decimal.Decimal `json:"cantidad"`
	PrecioUnitario  decimal.Decimal `json:"precioUnitario"`
	AfectacionIVA   int             `json:"afectacionIVA"` // 1 gravado, 2 exonerado, 3 exento, 4 parcial
	TasaIVA         int             `json:"tasaIVA"`       // 10, 5, 0
	UnidadMedida    int             `json:"unidadMedida"`
	DesUnidadMedida string          `json:"desUnidadMedida"`
	PropIVA         decimal.Decimal `json:"propIVA"` // gravado parcial
}

// documentResponse es el resultado de una emisión.
type documentResponse struct {
	CDC             string `json:"cdc"`
	Estado          string `json:"estado"`
	CodRes          string `json:"codRes"`
	ProtAut         string `json:"protAut,omitempty"`
	Mensaje         string `json:"mensaje,omitempty"`
	QR              string `json:"qr,omitempty"`
	NumeroDocumento string `json:"numeroDocumento"`
	Ambiente        string `json:"ambiente"`
}

// kudeResponse es el resultado de GET /v1/documents/{cdc}/kude. La generación
// del PDF es asíncrona (Gotenberg + subida a OCI tras el POST /v1/documents),
// por eso puede llegar en estado "pending" antes de tener kudeUrl.
type kudeResponse struct {
	CDC     string `json:"cdc"`
	Estado  string `json:"estado"` // pending | ready
	KudeURL string `json:"kudeUrl,omitempty"`
}

// cancelRequest es el body de POST /v1/documents/{cdc}/cancel.
type cancelRequest struct {
	Motivo string `json:"motivo"`
}

// inutilizacionRequest es el body de POST /v1/events/inutilizacion.
type inutilizacionRequest struct {
	Establecimiento string `json:"establecimiento"`
	PuntoExpedicion string `json:"punto_expedicion"`
	NumeroInicial   int    `json:"numeroInicial"`
	NumeroFinal     int    `json:"numeroFinal"`
	TipoDE          int    `json:"tipoDE"`
	Motivo          string `json:"motivo"`
}

// eventResponse es el resultado de un evento.
type eventResponse struct {
	Estado   string `json:"estado"`
	CodRes   string `json:"codRes"`
	ProtAut  string `json:"protAut,omitempty"`
	Mensaje  string `json:"mensaje,omitempty"`
	Ambiente string `json:"ambiente"`
}
