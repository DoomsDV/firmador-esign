package sifen

import (
	"encoding/xml"

	"github.com/shopspring/decimal"
)

const (
	SifenNS     = "http://ekuatia.set.gov.py/sifen/xsd"
	SifenXSI    = "http://www.w3.org/2001/XMLSchema-instance"
	SifenSchema = "https://ekuatia.set.gov.py/sifen/xsd siRecepDE_v150.xsd"
	DSigNS      = "http://www.w3.org/2000/09/xmldsig#"
	Version     = 150

	AlgExcC14N   = "http://www.w3.org/2001/10/xml-exc-c14n#"
	AlgEnveloped = "http://www.w3.org/2000/09/xmldsig#enveloped-signature"
	AlgRSASha256 = "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256"
	AlgSha256    = "http://www.w3.org/2001/04/xmlenc#sha256"
)

// RDE: raíz del Documento Electrónico.
type RDE struct {
	XMLName xml.Name `xml:"rDE"`
	XmlNs   string   `xml:"xmlns,attr"`
	Xsi     string   `xml:"xmlns:xsi,attr"`
	Schema  string   `xml:"xsi:schemaLocation,attr"`

	DVerFor   decimal.Decimal `xml:"dVerFor"`
	DE        *DE             `xml:"DE"`
	Signature *Signature      `xml:"http://www.w3.org/2000/09/xmldsig# Signature,omitempty"`
	GCamFuFD  *GCamFuFD       `xml:"gCamFuFD,omitempty"`
}

type DE struct {
	Id        string `xml:"Id,attr"`
	DDVId     int    `xml:"dDVId"`
	DFecFirma string `xml:"dFecFirma"`
	DSisFact  int    `xml:"dSisFact"`

	GOpeDE      GOpeDE      `xml:"gOpeDE"`
	GTimb       GTimb       `xml:"gTimb"`
	GDatGralOpe GDatGralOpe `xml:"gDatGralOpe"`
	GDtipDE     GDtipDE     `xml:"gDtipDE"`
	// gTotSub es obligatorio salvo en nota de remisión (C002=7), donde no se informa.
	GTotSub *GTotSub `xml:"gTotSub,omitempty"`
	// gCamDEAsoc (grupo H) es hijo directo de DE: documento(s) asociado(s)
	// para NCE/NDE, notas de remisión, etc.
	GCamDEAsoc []GCamDEAsoc `xml:"gCamDEAsoc,omitempty"`
}

type GOpeDE struct {
	ITipEmi    int    `xml:"iTipEmi"`
	DDesTipEmi string `xml:"dDesTipEmi"`
	DCodSeg    string `xml:"dCodSeg"`
}

type GTimb struct {
	ITiDE     int    `xml:"iTiDE"`
	DDesTiDE  string `xml:"dDesTiDE"`
	DNumTim   string `xml:"dNumTim"` // 8 dígitos (en TEST: RUC sin DV con ceros a la izquierda)
	DEst      string `xml:"dEst"`
	DPunExp   string `xml:"dPunExp"`
	DNumDoc   string `xml:"dNumDoc"`
	DSerieNum string `xml:"dSerieNum,omitempty"`
	DFeIniT   string `xml:"dFeIniT"`
}

type GDatGralOpe struct {
	DFeEmiDE string   `xml:"dFeEmiDE"`
	GOpeCom  *GOpeCom `xml:"gOpeCom,omitempty"`
	GEmis    GEmis    `xml:"gEmis"`
	GDatRec  GDatRec  `xml:"gDatRec"`
}

type GOpeCom struct {
	// D011/D012: obligatorio solo si C002 = 1 (FE) o 4 (autofactura); no informar
	// en NCE/NDE/remisión (validación 1216).
	ITipTra     int    `xml:"iTipTra,omitempty"`
	DDesTipTra  string `xml:"dDesTipTra,omitempty"`
	ITImp       int    `xml:"iTImp"`
	DDesTImp    string `xml:"dDesTImp"`
	CMoneOpe    string `xml:"cMoneOpe"`
	DDesMoneOpe string `xml:"dDesMoneOpe"`
	// Multimoneda (D017/D018): solo cuando cMoneOpe ≠ PYG.
	DCondTiCam int              `xml:"dCondTiCam,omitempty"` // 1=global, 2=por ítem
	DTiCam     *decimal.Decimal `xml:"dTiCam,omitempty"`     // tipo de cambio a PYG
}

type GEmis struct {
	DRucEm     string    `xml:"dRucEm"`
	DDVEmi     int       `xml:"dDVEmi"`
	ITipCont   int       `xml:"iTipCont"`
	CTipReg    int       `xml:"cTipReg,omitempty"` // opcional (1-8); 0 = no informar
	DNomEmi    string    `xml:"dNomEmi"`
	DNomFanEmi string    `xml:"dNomFanEmi,omitempty"`
	DDirEmi    string    `xml:"dDirEmi"`
	DNumCas    string    `xml:"dNumCas"`
	CDepEmi    int       `xml:"cDepEmi"`
	DDesDepEmi string    `xml:"dDesDepEmi"`
	CDisEmi    int       `xml:"cDisEmi,omitempty"`
	DDesDisEmi string    `xml:"dDesDisEmi,omitempty"`
	CCiuEmi    int       `xml:"cCiuEmi"`
	DDesCiuEmi string    `xml:"dDesCiuEmi"`
	DTelEmi    string    `xml:"dTelEmi,omitempty"`
	DEmailE    string    `xml:"dEmailE,omitempty"`
	GActEco    []GActEco `xml:"gActEco,omitempty"`
}

type GActEco struct {
	CActEco    string `xml:"cActEco"`
	DDesActEco string `xml:"dDesActEco"`
}

// GDatRec soporta receptor contribuyente (RUC) o no contribuyente (CI/otro ID).
type GDatRec struct {
	INatRec    int    `xml:"iNatRec"`
	ITiOpe     int    `xml:"iTiOpe"`
	CPaisRec   string `xml:"cPaisRec"`
	DDesPaisRe string `xml:"dDesPaisRe"`

	// Contribuyente
	ITiContRec int    `xml:"iTiContRec,omitempty"`
	DRucRec    string `xml:"dRucRec,omitempty"`
	DDVRec     int    `xml:"dDVRec,omitempty"`

	// No contribuyente
	ITipIDRec  int    `xml:"iTipIDRec,omitempty"`
	DDTipIDRec string `xml:"dDTipIDRec,omitempty"`
	DNumIDRec  string `xml:"dNumIDRec,omitempty"`

	DNomRec     string `xml:"dNomRec"`
	DDirRec     string `xml:"dDirRec,omitempty"`
	DNumCasRec  string `xml:"dNumCasRec,omitempty"`
	CDepRec     int    `xml:"cDepRec,omitempty"`
	DDesDepRec  string `xml:"dDesDepRec,omitempty"`
	CDisRec     int    `xml:"cDisRec,omitempty"`
	DDesDisRec  string `xml:"dDesDisRec,omitempty"`
	CCiuRec     int    `xml:"cCiuRec,omitempty"`
	DDesCiuRec  string `xml:"dDesCiuRec,omitempty"`
	DTelRec     string `xml:"dTelRec,omitempty"`
	DEmailRec   string `xml:"dEmailRec,omitempty"`
	DCodCliente string `xml:"dCodCliente,omitempty"`
}

// GDtipDE respeta el orden del XSD (DE_v150.xsd, tgDtipDE): gCamFE, gCamAE,
// gCamNCDE, gCamNRE, gCamCond, gCamItem, gTransp.
type GDtipDE struct {
	GCamFE   *GCamFE    `xml:"gCamFE,omitempty"`
	GCamAE   *GCamAE    `xml:"gCamAE,omitempty"`   // autofactura (E300)
	GCamNCDE *GCamNCDE  `xml:"gCamNCDE,omitempty"` // NCE/NDE: motivo de emisión (E400)
	GCamNRE  *GCamNRE   `xml:"gCamNRE,omitempty"`  // nota de remisión (E500)
	GCamCond *GCamCond  `xml:"gCamCond,omitempty"`
	GCamItem []GCamItem `xml:"gCamItem"`
	GTransp  *GTransp   `xml:"gTransp,omitempty"` // transporte (E900)
}

type GCamFE struct {
	IIndPres    int    `xml:"iIndPres"`
	DDesIndPres string `xml:"dDesIndPres"`
}

// GCamNCDE describe el motivo de emisión de una nota de crédito/débito (E1).
type GCamNCDE struct {
	IMotEmi    int    `xml:"iMotEmi"`
	DDesMotEmi string `xml:"dDesMotEmi"`
}

// GCamDEAsoc referencia el documento asociado (grupo H). Para NCE/NDE que
// referencian un DE electrónico basta iTipDocAso=1 + dCdCDERef (CDC de la FE).
type GCamDEAsoc struct {
	ITipDocAso    int    `xml:"iTipDocAso"`
	DDesTipDocAso string `xml:"dDesTipDocAso"`
	DCdCDERef     string `xml:"dCdCDERef,omitempty"` // CDC del DE electrónico referenciado
	DNTimDI       string `xml:"dNTimDI,omitempty"`
	DEstDocAso    string `xml:"dEstDocAso,omitempty"`
	DPExpDocAso   string `xml:"dPExpDocAso,omitempty"`
	DNumDocAso    string `xml:"dNumDocAso,omitempty"`
	ITipoDocAso   int    `xml:"iTipoDocAso,omitempty"`
	DDTipoDocAso  string `xml:"dDTipoDocAso,omitempty"`
	DFecEmiDI     string `xml:"dFecEmiDI,omitempty"`
	DNumComRet    string `xml:"dNumComRet,omitempty"`  // H012
	DNumResCF     string `xml:"dNumResCF,omitempty"`   // H013
	ITipCons      int    `xml:"iTipCons,omitempty"`    // H014 (1 no contrib., 2 microprod.)
	DDesTipCons   string `xml:"dDesTipCons,omitempty"` // H015
	DNumCons      int    `xml:"dNumCons,omitempty"`    // H016 (solo H014=2)
	DNumControl   string `xml:"dNumControl,omitempty"` // H017
}

type GCamCond struct {
	ICondOpe   int          `xml:"iCondOpe"`
	DDCondOpe  string       `xml:"dDCondOpe"`
	GPaConEIni []GPaConEIni `xml:"gPaConEIni,omitempty"`
	GPagCred   *GPagCred    `xml:"gPagCred,omitempty"`
}

type GPaConEIni struct {
	ITiPago     int             `xml:"iTiPago"`
	DDesTiPag   string          `xml:"dDesTiPag"`
	DMonTiPag   decimal.Decimal `xml:"dMonTiPag"`
	CMoneTiPag  string          `xml:"cMoneTiPag"`
	DDMoneTiPag string          `xml:"dDMoneTiPag"`
}

// GPagCred describe operación a crédito (E640-E649). Obligatorio si iCondOpe=2.
type GPagCred struct {
	ICondCred  int              `xml:"iCondCred"`
	DDCondCred string           `xml:"dDCondCred"`
	DPlazoCre  string           `xml:"dPlazoCre,omitempty"`
	DCuotas    int              `xml:"dCuotas,omitempty"`
	DMonEnt    *decimal.Decimal `xml:"dMonEnt,omitempty"` // solo si hay entrega inicial
	GCuotas    []GCuota         `xml:"gCuotas,omitempty"`
}

// GCuota describe cada cuota cuando iCondCred=2.
type GCuota struct {
	CMoneCuo  string          `xml:"cMoneCuo"`
	DDMoneCuo string          `xml:"dDMoneCuo"`
	DMonCuota decimal.Decimal `xml:"dMonCuota"`
	DVencCuo  string          `xml:"dVencCuo,omitempty"`
}

type GCamItem struct {
	DCodInt     string          `xml:"dCodInt"`
	DDesProSer  string          `xml:"dDesProSer"`
	CUniMed     int             `xml:"cUniMed"`
	DDesUniMed  string          `xml:"dDesUniMed"`
	DCantProSer decimal.Decimal `xml:"dCantProSer"`

	// En nota de remisión (C002=7) los ítems no llevan gValorItem ni gCamIVA.
	GValorItem *GValorItem `xml:"gValorItem,omitempty"`
	GCamIVA    *GCamIVA    `xml:"gCamIVA,omitempty"`
}

type GValorItem struct {
	DPUniProSer     decimal.Decimal  `xml:"dPUniProSer"`
	DTotBruOpeItem  decimal.Decimal  `xml:"dTotBruOpeItem"`
	GValorRestaItem *GValorRestaItem `xml:"gValorRestaItem,omitempty"`
}

// GValorRestaItem alineado al XML oficial (sin anticipos en cero).
type GValorRestaItem struct {
	DDescItem    decimal.Decimal `xml:"dDescItem"`
	DPorcDesIt   decimal.Decimal `xml:"dPorcDesIt"`
	DDescGloItem decimal.Decimal `xml:"dDescGloItem"`
	DTotOpeItem  decimal.Decimal `xml:"dTotOpeItem"`
}

type GCamIVA struct {
	IAfecIVA    int             `xml:"iAfecIVA"`
	DDesAfecIVA string          `xml:"dDesAfecIVA"`
	DPropIVA    decimal.Decimal `xml:"dPropIVA"`
	DTasaIVA    decimal.Decimal `xml:"dTasaIVA"`
	DBasGravIVA decimal.Decimal `xml:"dBasGravIVA"`
	DLiqIVAItem decimal.Decimal `xml:"dLiqIVAItem"`
	DBasExe     decimal.Decimal `xml:"dBasExe"` // requerido por DE_v150 (0 si gravado total)
}

type GTotSub struct {
	DSubExe        decimal.Decimal  `xml:"dSubExe"`
	DSubExo        decimal.Decimal  `xml:"dSubExo"`
	DSub5          decimal.Decimal  `xml:"dSub5"`
	DSub10         decimal.Decimal  `xml:"dSub10"`
	DTotOpe        decimal.Decimal  `xml:"dTotOpe"`
	DTotDesc       decimal.Decimal  `xml:"dTotDesc"`
	DTotDescGlotem decimal.Decimal  `xml:"dTotDescGlotem"`
	DTotAntItem    decimal.Decimal  `xml:"dTotAntItem"`
	DTotAnt        decimal.Decimal  `xml:"dTotAnt"`
	DPorcDescTotal decimal.Decimal  `xml:"dPorcDescTotal"`
	DDescTotal     decimal.Decimal  `xml:"dDescTotal"`
	DAnticipo      decimal.Decimal  `xml:"dAnticipo"`
	DRedon         decimal.Decimal  `xml:"dRedon"`
	DTotGralOpe    decimal.Decimal  `xml:"dTotGralOpe"`
	DTotIVA5       decimal.Decimal  `xml:"dIVA5"`
	DTotIVA10      decimal.Decimal  `xml:"dIVA10"`
	DTotIVA        decimal.Decimal  `xml:"dTotIVA"`
	DBaseGrav5     decimal.Decimal  `xml:"dBaseGrav5"`
	DBaseGrav10    decimal.Decimal  `xml:"dBaseGrav10"`
	DTBasGraIVA    decimal.Decimal  `xml:"dTBasGraIVA"`
	DTotalGs       *decimal.Decimal `xml:"dTotalGs,omitempty"` // solo si cMoneOpe ≠ PYG (F023)
}

type GCamFuFD struct {
	DCarQR   string `xml:"dCarQR"`
	DInfAdic string `xml:"dInfAdic,omitempty"`
}

type Signature struct {
	XMLName        xml.Name   `xml:"http://www.w3.org/2000/09/xmldsig# Signature"`
	SignedInfo     SignedInfo `xml:"SignedInfo"`
	SignatureValue string     `xml:"SignatureValue"`
	KeyInfo        KeyInfo    `xml:"KeyInfo"`
}

type SignedInfo struct {
	CanonicalizationMethod Algorithm `xml:"CanonicalizationMethod"`
	SignatureMethod        Algorithm `xml:"SignatureMethod"`
	Reference              Reference `xml:"Reference"`
}

type Reference struct {
	URI          string     `xml:"URI,attr"`
	Transforms   Transforms `xml:"Transforms"`
	DigestMethod Algorithm  `xml:"DigestMethod"`
	DigestValue  string     `xml:"DigestValue"`
}

type Transforms struct {
	Transform []Algorithm `xml:"Transform"`
}

type Algorithm struct {
	Algorithm string `xml:"Algorithm,attr"`
}

type KeyInfo struct {
	X509Data X509Data `xml:"X509Data"`
}

type X509Data struct {
	X509Certificate  string            `xml:"X509Certificate"`
	X509IssuerSerial *X509IssuerSerial `xml:"X509IssuerSerial,omitempty"`
}

type X509IssuerSerial struct {
	X509IssuerName   string `xml:"X509IssuerName"`
	X509SerialNumber string `xml:"X509SerialNumber"`
}
