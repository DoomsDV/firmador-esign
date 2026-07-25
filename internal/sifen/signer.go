package sifen

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

// QRBuilder arma dCarQR a partir del DigestValue ya calculado.
type QRBuilder func(digestValue string) (string, error)

// FirmarYSerializar firma el DE en etree y serializa UNA sola vez el XML final
// (mismo DOM firmado = el enviado a SIFEN). Evita el rechazo 0141 por re-Marshal.
func FirmarYSerializar(rde *RDE, cert *DigitalCertificate, buildQR QRBuilder) (digestValue string, xmlOut []byte, err error) {
	if rde == nil || rde.DE == nil {
		return "", nil, fmt.Errorf("rDE/DE nulo")
	}
	if rde.DE.Id == "" {
		return "", nil, fmt.Errorf("el documento DE no tiene ID (CDC)")
	}
	if cert == nil || cert.PrivateKey == nil || cert.Certificate == nil {
		return "", nil, fmt.Errorf("certificado inválido")
	}
	if buildQR == nil {
		return "", nil, fmt.Errorf("buildQR nulo")
	}

	rde.Signature = nil
	rde.GCamFuFD = nil

	raw, err := xml.Marshal(rde)
	if err != nil {
		return "", nil, fmt.Errorf("marshal rDE: %w", err)
	}

	doc := etree.NewDocument()
	doc.WriteSettings.CanonicalText = true
	if err := doc.ReadFromBytes(raw); err != nil {
		return "", nil, fmt.Errorf("parse etree: %w", err)
	}

	root := doc.Root()
	if root == nil {
		return "", nil, fmt.Errorf("rDE raíz nula")
	}

	deEl := root.FindElement("./DE")
	if deEl == nil {
		return "", nil, fmt.Errorf("no se encontró DE")
	}

	sigEl, digestValue, err := signEnvelopedElement(deEl, rde.DE.Id, cert)
	if err != nil {
		return "", nil, err
	}

	insertAfter(root, deEl, sigEl)

	qrURL, err := buildQR(digestValue)
	if err != nil {
		return "", nil, fmt.Errorf("QR: %w", err)
	}

	fu := etree.NewElement("gCamFuFD")
	car := fu.CreateElement("dCarQR")
	car.SetText(qrURL)
	root.AddChild(fu)

	out, err := doc.WriteToBytes()
	if err != nil {
		return "", nil, fmt.Errorf("serializar: %w", err)
	}

	xmlOut = append([]byte(xml.Header), out...)
	if err := VerifySignedXML(xmlOut, cert); err != nil {
		return "", nil, fmt.Errorf("auto-verificación firma falló: %w", err)
	}

	return digestValue, xmlOut, nil
}

// signEnvelopedElement firma un elemento por su Id con el patrón SIFEN: calcula
// el digest Exc-C14N del elemento (con xmlns default sifen inyectado) y arma el
// nodo Signature (enveloped + exc-c14n, RSA-SHA256). Reutilizado por el DE y por
// los eventos. goxmldsig no hereda xmlns del padre, por eso se inyecta en una
// copia; el DOM enviado mantiene el elemento sin xmlns local.
func signEnvelopedElement(el *etree.Element, id string, cert *DigitalCertificate) (sig *etree.Element, digestValue string, err error) {
	canon, err := exclusiveC14NWithDefaultNS(el, SifenNS)
	if err != nil {
		return nil, "", fmt.Errorf("C14N %s: %w", el.Tag, err)
	}
	sum := sha256.Sum256(canon)
	digestValue = base64.StdEncoding.EncodeToString(sum[:])

	sig, err = buildSignatureElement(id, digestValue, cert)
	if err != nil {
		return nil, "", err
	}
	return sig, digestValue, nil
}

func buildSignatureElement(cdc, digestValue string, cert *DigitalCertificate) (*etree.Element, error) {
	sig := etree.NewElement("Signature")
	sig.CreateAttr("xmlns", DSigNS)

	si := sig.CreateElement("SignedInfo")

	cm := si.CreateElement("CanonicalizationMethod")
	cm.CreateAttr("Algorithm", AlgExcC14N)

	sm := si.CreateElement("SignatureMethod")
	sm.CreateAttr("Algorithm", AlgRSASha256)

	ref := si.CreateElement("Reference")
	ref.CreateAttr("URI", "#"+cdc)

	transforms := ref.CreateElement("Transforms")
	t1 := transforms.CreateElement("Transform")
	t1.CreateAttr("Algorithm", AlgEnveloped)
	t2 := transforms.CreateElement("Transform")
	t2.CreateAttr("Algorithm", AlgExcC14N)

	dm := ref.CreateElement("DigestMethod")
	dm.CreateAttr("Algorithm", AlgSha256)

	dv := ref.CreateElement("DigestValue")
	dv.SetText(digestValue)

	// Firma sobre C14N de SignedInfo CON xmlns xmldsig (herencia del Signature).
	siCanon, err := exclusiveC14NWithDefaultNS(si, DSigNS)
	if err != nil {
		return nil, fmt.Errorf("C14N SignedInfo: %w", err)
	}
	siHash := sha256.Sum256(siCanon)
	sigBytes, err := rsa.SignPKCS1v15(rand.Reader, cert.PrivateKey, crypto.SHA256, siHash[:])
	if err != nil {
		return nil, fmt.Errorf("RSA sign: %w", err)
	}

	sv := sig.CreateElement("SignatureValue")
	sv.SetText(base64.StdEncoding.EncodeToString(sigBytes))

	ki := sig.CreateElement("KeyInfo")
	xd := ki.CreateElement("X509Data")
	xc := xd.CreateElement("X509Certificate")
	xc.SetText(base64.StdEncoding.EncodeToString(cert.Certificate.Raw))

	// SIFEN/ejemplo oficial incluye IssuerSerial junto al certificado.
	xis := xd.CreateElement("X509IssuerSerial")
	xin := xis.CreateElement("X509IssuerName")
	xin.SetText(formatIssuerDN(cert.Certificate.Issuer))
	xsn := xis.CreateElement("X509SerialNumber")
	xsn.SetText(cert.Certificate.SerialNumber.String())

	return sig, nil
}

func formatIssuerDN(name pkix.Name) string {
	// Formato similar al ejemplo DNIT (CN=,O=,C=...).
	return strings.TrimSpace(name.String())
}

// exclusiveC14NWithDefaultNS canóniza Exc-C14N sobre una copia con xmlns default
// inyectado. Necesario porque goxmldsig.TransformExcC14n no hereda xmlns del padre.
func exclusiveC14NWithDefaultNS(el *etree.Element, defaultNS string) ([]byte, error) {
	cp := el.Copy()
	if defaultNS != "" {
		_ = cp.RemoveAttr("xmlns")
		cp.CreateAttr("xmlns", defaultNS)
	}
	canon := dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	return canon.Canonicalize(cp)
}

func insertAfter(parent, after, neu *etree.Element) {
	if after == nil {
		parent.AddChild(neu)
		return
	}
	parent.InsertChildAt(after.Index()+1, neu)
}

// VerifySignedXML comprueba DigestValue del DE y SignatureValue del SignedInfo
// con el mismo Exc-C14N (xmlns inyectado) usado al firmar.
func VerifySignedXML(xmlDoc []byte, cert *DigitalCertificate) error {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(stripXMLHeader(xmlDoc)); err != nil {
		return err
	}
	root := doc.Root()
	if root == nil {
		return fmt.Errorf("sin raíz")
	}
	deEl := root.FindElement("./DE")
	sigEl := root.FindElement("./Signature")
	if deEl == nil || sigEl == nil {
		return fmt.Errorf("faltan DE o Signature")
	}

	deCanon, err := exclusiveC14NWithDefaultNS(deEl, SifenNS)
	if err != nil {
		return err
	}
	wantDigest := base64.StdEncoding.EncodeToString(sha256Sum(deCanon))

	dvEl := sigEl.FindElement("./SignedInfo/Reference/DigestValue")
	if dvEl == nil {
		return fmt.Errorf("sin DigestValue")
	}
	if strings.TrimSpace(dvEl.Text()) != wantDigest {
		return fmt.Errorf("DigestValue no coincide con C14N del DE")
	}

	si := sigEl.FindElement("./SignedInfo")
	if si == nil {
		return fmt.Errorf("sin SignedInfo")
	}
	siCanon, err := exclusiveC14NWithDefaultNS(si, DSigNS)
	if err != nil {
		return err
	}
	svEl := sigEl.FindElement("./SignatureValue")
	if svEl == nil {
		return fmt.Errorf("sin SignatureValue")
	}
	sigBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(svEl.Text()))
	if err != nil {
		return err
	}
	sum := sha256.Sum256(siCanon)
	pub, ok := cert.Certificate.PublicKey.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("cert sin RSA public key")
	}
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sigBytes); err != nil {
		return fmt.Errorf("SignatureValue inválido: %w", err)
	}
	return nil
}

func sha256Sum(b []byte) []byte {
	s := sha256.Sum256(b)
	return s[:]
}

func stripXMLHeader(b []byte) []byte {
	s := strings.TrimSpace(string(b))
	if strings.HasPrefix(s, "<?xml") {
		if i := strings.Index(s, "?>"); i >= 0 {
			return []byte(strings.TrimSpace(s[i+2:]))
		}
	}
	return []byte(s)
}

// Compat.
func formatIssuerName(name pkix.Name) string {
	return formatIssuerDN(name)
}
