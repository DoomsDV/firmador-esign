package sifen

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

const soapNS = "http://www.w3.org/2003/05/soap-envelope"

// Client SOAP mTLS hacia sifen-test únicamente.
type Client struct {
	HTTP    *http.Client
	SyncURL string
}

// NewTestClient crea cliente HTTP con mTLS; SyncURL debe ser sifen-test.
func NewTestClient(p12Path, p12Password, syncURL string) (*Client, error) {
	if err := AssertSafeTestURL(syncURL); err != nil {
		return nil, err
	}

	tlsCert, err := loadTLSCertificate(p12Path, p12Password)
	if err != nil {
		return nil, err
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
			MinVersion:   tls.VersionTLS12,
		},
	}

	return &Client{
		HTTP: &http.Client{
			Timeout:   60 * time.Second,
			Transport: tr,
		},
		SyncURL: syncURL,
	}, nil
}

func loadTLSCertificate(path, password string) (tls.Certificate, error) {
	p12Data, err := os.ReadFile(path)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("leer p12: %w", err)
	}
	key, leaf, cas, err := pkcs12.DecodeChain(p12Data, password)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("decode p12: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return tls.Certificate{}, fmt.Errorf("clave no RSA")
	}
	tc := tls.Certificate{
		Certificate: [][]byte{leaf.Raw},
		PrivateKey:  rsaKey,
		Leaf:        leaf,
	}
	for _, ca := range cas {
		tc.Certificate = append(tc.Certificate, ca.Raw)
	}
	return tc, nil
}

// RecibirDESync envía un rDE firmado al WS síncrono de test (siRecepDE).
func (c *Client) RecibirDESync(ctx context.Context, envioID int64, rdeXML []byte) (statusCode int, respBody []byte, err error) {
	if c == nil || c.HTTP == nil {
		return 0, nil, fmt.Errorf("cliente SOAP nulo")
	}
	if err := AssertSafeTestURL(c.SyncURL); err != nil {
		return 0, nil, err
	}

	envelope, err := buildSiRecepDEEnvelope(envioID, rdeXML)
	if err != nil {
		return 0, nil, err
	}
	return c.postSOAP(ctx, c.SyncURL, envelope)
}

// RecibirEventoSync envía un evento firmado al WS síncrono de eventos de test
// (siRecepEvento). eventoFirmado es el XML de <gGroupGesEve> producido por
// FirmarEvento.
func (c *Client) RecibirEventoSync(ctx context.Context, envioID int64, eventoURL string, eventoFirmado []byte) (statusCode int, respBody []byte, err error) {
	if c == nil || c.HTTP == nil {
		return 0, nil, fmt.Errorf("cliente SOAP nulo")
	}
	if err := AssertSafeTestURL(eventoURL); err != nil {
		return 0, nil, err
	}

	envelope, err := buildSiRecepEventoEnvelope(envioID, eventoFirmado)
	if err != nil {
		return 0, nil, err
	}
	return c.postSOAP(ctx, eventoURL, envelope)
}

// postSOAP hace el POST SOAP 1.2 con mTLS y respeta el contexto.
func (c *Client) postSOAP(ctx context.Context, url string, envelope []byte) (int, []byte, error) {
	if err := AssertSafeTestURL(url); err != nil {
		return 0, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(envelope))
	if err != nil {
		return 0, nil, err
	}
	// SOAP 1.2 (binding Soap12 del WSDL): media type oficial.
	// Envelope: tags soap: compactos, sin whitespace entre elementos.
	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")
	req.Header.Set("Accept", "application/soap+xml, application/xml")
	req.ContentLength = int64(len(envelope))

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("POST sifen-test: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("leer respuesta: %w", err)
	}
	return resp.StatusCode, body, nil
}

// buildSiRecepDEEnvelope arma el SOAP 1.2 compacto (sin whitespace entre tags).
func buildSiRecepDEEnvelope(envioID int64, rdeXML []byte) ([]byte, error) {
	payload, err := stripXMLDecl(rdeXML)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.Grow(len(payload) + 256)
	// Declaración + envelope compacto (sin espacios/newlines entre elementos).
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	buf.WriteString(`<soap:Envelope xmlns:soap="` + soapNS + `">`)
	buf.WriteString(`<soap:Header></soap:Header>`)
	buf.WriteString(`<soap:Body>`)
	buf.WriteString(`<rEnviDe xmlns="` + SifenNS + `">`)
	buf.WriteString(`<dId>`)
	buf.WriteString(fmt.Sprintf("%d", envioID))
	buf.WriteString(`</dId>`)
	buf.WriteString(`<xDE>`)
	buf.Write(payload)
	buf.WriteString(`</xDE>`)
	buf.WriteString(`</rEnviDe>`)
	buf.WriteString(`</soap:Body>`)
	buf.WriteString(`</soap:Envelope>`)
	return buf.Bytes(), nil
}

// buildSiRecepEventoEnvelope arma el SOAP 1.2 del WS de eventos. eventoFirmado
// es el XML de <gGroupGesEve> ya firmado; se inserta dentro de dEvReg.
func buildSiRecepEventoEnvelope(envioID int64, eventoFirmado []byte) ([]byte, error) {
	payload := bytes.TrimSpace(eventoFirmado)
	payload = bytes.TrimPrefix(payload, []byte{0xEF, 0xBB, 0xBF})
	if bytes.HasPrefix(payload, []byte("<?xml")) {
		i := bytes.Index(payload, []byte("?>"))
		if i < 0 {
			return nil, fmt.Errorf("declaración XML incompleta en evento")
		}
		payload = bytes.TrimSpace(payload[i+2:])
	}
	if !bytes.HasPrefix(payload, []byte("<gGroupGesEve")) {
		return nil, fmt.Errorf("payload de evento no empieza con <gGroupGesEve>")
	}

	var buf bytes.Buffer
	buf.Grow(len(payload) + 256)
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	buf.WriteString(`<soap:Envelope xmlns:soap="` + soapNS + `">`)
	buf.WriteString(`<soap:Header></soap:Header>`)
	buf.WriteString(`<soap:Body>`)
	buf.WriteString(`<rEnviEventoDe xmlns="` + SifenNS + `">`)
	buf.WriteString(`<dId>`)
	buf.WriteString(fmt.Sprintf("%d", envioID))
	buf.WriteString(`</dId>`)
	buf.WriteString(`<dEvReg>`)
	buf.Write(payload)
	buf.WriteString(`</dEvReg>`)
	buf.WriteString(`</rEnviEventoDe>`)
	buf.WriteString(`</soap:Body>`)
	buf.WriteString(`</soap:Envelope>`)
	return buf.Bytes(), nil
}

func stripXMLDecl(rdeXML []byte) ([]byte, error) {
	payload := bytes.TrimSpace(rdeXML)
	if len(payload) == 0 {
		return nil, fmt.Errorf("rDE XML vacío")
	}
	if bytes.HasPrefix(payload, []byte("<?xml")) {
		i := bytes.Index(payload, []byte("?>"))
		if i < 0 {
			return nil, fmt.Errorf("declaración XML incompleta en rDE")
		}
		payload = bytes.TrimSpace(payload[i+2:])
	}
	if !bytes.HasPrefix(payload, []byte("<rDE")) {
		return nil, fmt.Errorf("payload no empieza con <rDE>")
	}
	// Evitar BOMs / bytes nulos colados.
	payload = bytes.TrimPrefix(payload, []byte{0xEF, 0xBB, 0xBF})
	if strings.ContainsRune(string(payload[:min(32, len(payload))]), '\u0000') {
		return nil, fmt.Errorf("payload contiene NUL")
	}
	return payload, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
