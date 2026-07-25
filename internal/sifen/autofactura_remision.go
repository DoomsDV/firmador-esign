package sifen

import "fmt"

// --- Autofactura electrónica (gCamAE, iTiDE=4) ---

// GCamAE describe al vendedor no contribuyente y el lugar de la transacción de
// una autofactura (DE_v150.xsd, tgCamAE, orden estricto de campos).
type GCamAE struct {
	INatVen     int    `xml:"iNatVen"`    // 1 no contribuyente, 2 extranjero
	DDesNatVen  string `xml:"dDesNatVen"` // descripción de iNatVen
	ITipIDVen   int    `xml:"iTipIDVen"`  // tipo de documento del vendedor
	DDTipIDVen  string `xml:"dDTipIDVen"`
	DNumIDVen   string `xml:"dNumIDVen"`
	DNomVen     string `xml:"dNomVen"`
	DDirVen     string `xml:"dDirVen"`
	DNumCasVen  string `xml:"dNumCasVen"`
	CDepVen     int    `xml:"cDepVen"`
	DDesDepVen  string `xml:"dDesDepVen"`
	CDisVen     int    `xml:"cDisVen,omitempty"`
	DDesDisVen  string `xml:"dDesDisVen,omitempty"`
	CCiuVen     int    `xml:"cCiuVen"`
	DDesCiuVen  string `xml:"dDesCiuVen"`
	DDirProv    string `xml:"dDirProv"` // lugar donde se realizó la transacción
	CDepProv    int    `xml:"cDepProv"`
	DDesDepProv string `xml:"dDesDepProv"`
	CDisProv    int    `xml:"cDisProv,omitempty"`
	DDesDisProv string `xml:"dDesDisProv,omitempty"`
	CCiuProv    int    `xml:"cCiuProv"`
	DDesCiuProv string `xml:"dDesCiuProv"`
}

// natVenDescs mapea iNatVen a su descripción oficial.
var natVenDescs = map[int]string{
	1: "No contribuyente",
	2: "Extranjero",
}

// UbicacionAE agrupa una ubicación (vendedor o lugar de transacción).
type UbicacionAE struct {
	Direccion       string
	NumeroCasa      string
	Departamento    int
	DesDepartamento string
	Distrito        int
	DesDistrito     string
	Ciudad          int
	DesCiudad       string
}

// NewCamAE arma el gCamAE validando naturaleza y tipo de identificación del
// vendedor. ubicVen es el domicilio del vendedor y ubicTrans el lugar donde se
// realizó la transacción.
func NewCamAE(natVen, tipoIDVen int, numIDVen, nombreVen string, ubicVen, ubicTrans UbicacionAE) (*GCamAE, error) {
	desNat, ok := natVenDescs[natVen]
	if !ok {
		return nil, fmt.Errorf("iNatVen inválido: %d", natVen)
	}
	desID, err := tipIDRecDesc(tipoIDVen)
	if err != nil {
		return nil, fmt.Errorf("tipo de identidad del vendedor: %w", err)
	}
	if len(nombreVen) < 4 {
		return nil, fmt.Errorf("nombre del vendedor inválido")
	}
	if numIDVen == "" {
		return nil, fmt.Errorf("número de identidad del vendedor vacío")
	}
	return &GCamAE{
		INatVen: natVen, DDesNatVen: desNat,
		ITipIDVen: tipoIDVen, DDTipIDVen: desID, DNumIDVen: numIDVen,
		DNomVen:    nombreVen,
		DDirVen:    ubicVen.Direccion,
		DNumCasVen: defCasa(ubicVen.NumeroCasa),
		CDepVen:    ubicVen.Departamento, DDesDepVen: ubicVen.DesDepartamento,
		CDisVen: ubicVen.Distrito, DDesDisVen: ubicVen.DesDistrito,
		CCiuVen: ubicVen.Ciudad, DDesCiuVen: ubicVen.DesCiudad,
		DDirProv: ubicTrans.Direccion,
		CDepProv: ubicTrans.Departamento, DDesDepProv: ubicTrans.DesDepartamento,
		CDisProv: ubicTrans.Distrito, DDesDisProv: ubicTrans.DesDistrito,
		CCiuProv: ubicTrans.Ciudad, DDesCiuProv: ubicTrans.DesCiudad,
	}, nil
}

// --- Nota de remisión electrónica (gCamNRE + gTransp, iTiDE=7) ---

// GCamNRE describe el motivo de emisión de la nota de remisión (tgCamNRE).
type GCamNRE struct {
	IMotEmiNR     int    `xml:"iMotEmiNR"`
	DDesMotEmiNR  string `xml:"dDesMotEmiNR"`
	IRespEmiNR    int    `xml:"iRespEmiNR"`
	DDesRespEmiNR string `xml:"dDesRespEmiNR"`
	DKmR          int    `xml:"dKmR"` // kilómetros estimados de recorrido (1-99999)
	DFecEm        string `xml:"dFecEm,omitempty"`
}

// GTransp describe el transporte de las mercaderías (tgTransp).
type GTransp struct {
	ITipTrans    int    `xml:"iTipTrans,omitempty"`
	DDesTipTrans string `xml:"dDesTipTrans,omitempty"`
	IModTrans    int    `xml:"iModTrans"`
	DDesModTrans string `xml:"dDesModTrans"`
	IRespFlete   int    `xml:"iRespFlete"`
	CCondNeg     string `xml:"cCondNeg,omitempty"`
	DNuManif     string `xml:"dNuManif,omitempty"`
	DNuDespImp   string `xml:"dNuDespImp,omitempty"`
	DIniTras     string `xml:"dIniTras,omitempty"`
	DFinTras     string `xml:"dFinTras,omitempty"`
	CPaisDest    string `xml:"cPaisDest,omitempty"`
	DDesPaisDest string `xml:"dDesPaisDest,omitempty"`

	GCamSal   *GCamSal   `xml:"gCamSal,omitempty"`
	GCamEnt   []GCamEnt  `xml:"gCamEnt,omitempty"`
	GVehTras  []GVehTras `xml:"gVehTras,omitempty"`
	GCamTrans *GCamTrans `xml:"gCamTrans,omitempty"`
}

// GCamSal identifica el local de salida de las mercaderías (tgCamSal).
type GCamSal struct {
	DDirLocSal string `xml:"dDirLocSal"`
	DNumCasSal string `xml:"dNumCasSal"`
	DComp1Sal  string `xml:"dComp1Sal,omitempty"`
	DComp2Sal  string `xml:"dComp2Sal,omitempty"`
	CDepSal    int    `xml:"cDepSal"`
	DDesDepSal string `xml:"dDesDepSal"`
	CDisSal    int    `xml:"cDisSal,omitempty"`
	DDesDisSal string `xml:"dDesDisSal,omitempty"`
	CCiuSal    int    `xml:"cCiuSal"`
	DDesCiuSal string `xml:"dDesCiuSal"`
	DTelSal    string `xml:"dTelSal,omitempty"`
}

// GCamEnt identifica el local de entrega de las mercaderías (tgCamEnt).
type GCamEnt struct {
	DDirLocEnt string `xml:"dDirLocEnt"`
	DNumCasEnt string `xml:"dNumCasEnt"`
	DComp1Ent  string `xml:"dComp1Ent,omitempty"`
	DComp2Ent  string `xml:"dComp2Ent,omitempty"`
	CDepEnt    int    `xml:"cDepEnt"`
	DDesDepEnt string `xml:"dDesDepEnt"`
	CDisEnt    int    `xml:"cDisEnt,omitempty"`
	DDesDisEnt string `xml:"dDesDisEnt,omitempty"`
	CCiuEnt    int    `xml:"cCiuEnt"`
	DDesCiuEnt string `xml:"dDesCiuEnt"`
	DTelEnt    string `xml:"dTelEnt,omitempty"`
}

// GVehTras identifica al vehículo del traslado (tgVehTras).
type GVehTras struct {
	DTiVehTras  string `xml:"dTiVehTras"`
	DMarVeh     string `xml:"dMarVeh"`
	DTipIdenVeh int    `xml:"dTipIdenVeh"` // 1 nº identificación, 2 nº matrícula
	DNroIDVeh   string `xml:"dNroIDVeh,omitempty"`
	DAdicVeh    string `xml:"dAdicVeh,omitempty"`
	DNroMatVeh  string `xml:"dNroMatVeh,omitempty"`
	DNroVuelo   string `xml:"dNroVuelo,omitempty"`
}

// GCamTrans identifica al transportista (tgCamTrans).
type GCamTrans struct {
	INatTrans    int    `xml:"iNatTrans"` // 1 contribuyente, 2 no contribuyente
	DNomTrans    string `xml:"dNomTrans"`
	DRucTrans    string `xml:"dRucTrans,omitempty"`
	DDVTrans     int    `xml:"dDVTrans,omitempty"`
	ITipIDTrans  int    `xml:"iTipIDTrans,omitempty"`
	DDTipIDTrans string `xml:"dDTipIDTrans,omitempty"`
	DNumIDTrans  string `xml:"dNumIDTrans,omitempty"`
	CNacTrans    string `xml:"cNacTrans,omitempty"`
	DDesNacTrans string `xml:"dDesNacTrans,omitempty"`
	DNumIDChof   string `xml:"dNumIDChof"`
	DNomChof     string `xml:"dNomChof"`
	DDomFisc     string `xml:"dDomFisc"`
	DDirChof     string `xml:"dDirChof"`
	DNombAg      string `xml:"dNombAg,omitempty"`
	DRucAg       string `xml:"dRucAg,omitempty"`
	DDVAg        int    `xml:"dDVAg,omitempty"`
	DDirAge      string `xml:"dDirAge,omitempty"`
}

// motivEmiNRDescs mapea iMotEmiNR a su descripción (E501).
var motivEmiNRDescs = map[int]string{
	1:  "Traslado por ventas",
	2:  "Traslado por consignación",
	3:  "Exportación",
	4:  "Traslado por compra",
	5:  "Importación",
	6:  "Traslado por devolución",
	7:  "Traslado entre locales de la empresa",
	8:  "Traslado de bienes por transformación",
	9:  "Traslado de bienes por reparación",
	10: "Traslado por emisor móvil",
	11: "Exhibición o Demostración",
	12: "Participación en ferias",
	13: "Traslado de encomienda",
	14: "Decomiso",
	99: "Otro",
}

// respEmiNRDescs mapea iRespEmiNR a su descripción (E503).
var respEmiNRDescs = map[int]string{
	1: "Emisor de la factura",
	2: "Poseedor de la factura y bienes",
	3: "Empresa transportista",
	4: "Despachante de Aduanas",
	5: "Agente de transporte o intermediario",
}

// modTransDescs mapea iModTrans a su descripción (E903).
var modTransDescs = map[int]string{
	1: "Terrestre",
	2: "Fluvial",
	3: "Aéreo",
	4: "Multimodal",
}

// tipoTransDescs mapea iTipTrans a su descripción (E901).
var tipoTransDescs = map[int]string{
	1: "Propio",
	2: "Tercero",
}

// respFleteDescs mapea iRespFlete a su descripción (E905).
var respFleteDescs = map[int]string{
	1: "Emisor de la Factura Electrónica",
	2: "Receptor de la Factura Electrónica",
	3: "Tercero",
	4: "Agente intermediario del transporte (cuando intervenga)",
}

// DescTipoTransporte devuelve la descripción del tipo de transporte (E902).
func DescTipoTransporte(t int) (string, error) {
	d, ok := tipoTransDescs[t]
	if !ok {
		return "", fmt.Errorf("iTipTrans inválido: %d", t)
	}
	return d, nil
}

// DescRespFlete devuelve la descripción del responsable del flete (E906).
func DescRespFlete(r int) (string, error) {
	d, ok := respFleteDescs[r]
	if !ok {
		return "", fmt.Errorf("iRespFlete inválido: %d", r)
	}
	return d, nil
}

// DescMotivoRemision devuelve la descripción del motivo de emisión de remisión.
func DescMotivoRemision(motivo int) (string, error) {
	d, ok := motivEmiNRDescs[motivo]
	if !ok {
		return "", fmt.Errorf("iMotEmiNR inválido: %d", motivo)
	}
	return d, nil
}

// NewCamNRE arma el gCamNRE de una nota de remisión.
func NewCamNRE(motivo, responsable, kmRecorrido int) (*GCamNRE, error) {
	desMot, err := DescMotivoRemision(motivo)
	if err != nil {
		return nil, err
	}
	desResp, ok := respEmiNRDescs[responsable]
	if !ok {
		return nil, fmt.Errorf("iRespEmiNR inválido: %d", responsable)
	}
	if kmRecorrido < 1 || kmRecorrido > 99999 {
		return nil, fmt.Errorf("dKmR debe estar en 1-99999, got %d", kmRecorrido)
	}
	return &GCamNRE{
		IMotEmiNR: motivo, DDesMotEmiNR: desMot,
		IRespEmiNR: responsable, DDesRespEmiNR: desResp,
		DKmR: kmRecorrido,
	}, nil
}

// DescModalidadTransporte devuelve la descripción de la modalidad de transporte.
func DescModalidadTransporte(mod int) (string, error) {
	d, ok := modTransDescs[mod]
	if !ok {
		return "", fmt.Errorf("iModTrans inválido: %d", mod)
	}
	return d, nil
}

func defCasa(v string) string {
	if v == "" {
		return "0"
	}
	return v
}
