package retry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DoomsDV/firmador-e/internal/sifen"
	"github.com/DoomsDV/firmador-e/internal/tenant"
)

func TestPersistIncludesXMLFirmado(t *testing.T) {
	t.Parallel()

	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/documents") {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{}})
	}))
	defer server.Close()

	w := New(tenant.NewResolver(tenant.NewORDSClient(server.URL, "tok"), make([]byte, 32), 0), Config{})
	xml := `<rDE xmlns="http://ekuatia.set.gov.py/sifen/xsd"><dVerFor>150</dVerFor><DE Id="X"><dDVId>1</dDVId></DE></rDE>`
	doc := &tenant.PendingRetryDoc{
		ClientID:        1,
		Environment:     "TEST",
		CDC:             "X",
		NumDocumento:    "0000001",
		Establecimiento: "001",
		PuntoExpedicion: "001",
		TipoDE:          1,
		XMLFirmado:      xml,
		QRURL:           "https://qr.test",
	}
	if err := w.persist(t.Context(), doc, sifen.Result{CodRes: "0260"}, "APROBADO"); err != nil {
		t.Fatalf("persist: %v", err)
	}
	if got["xml_firmado"] != xml {
		t.Fatalf("retry persist omitted xml_firmado: got %#v", got["xml_firmado"])
	}
	if got["from_retry"] != true {
		t.Fatalf("from_retry = %v, want true", got["from_retry"])
	}
}

type fakeRetryClient struct {
	queryBodies [][]byte
	queryErr    error
	queryCalls  int
	sendCalls   int
	sentXML     []byte
	sendBody    []byte
}

func (c *fakeRetryClient) ConsultarDE(context.Context, int64, string, string) (int, []byte, error) {
	c.queryCalls++
	if c.queryErr != nil {
		return 0, nil, c.queryErr
	}
	if len(c.queryBodies) == 0 {
		return 200, []byte(`<rRes><dCodRes>0420</dCodRes></rRes>`), nil
	}
	index := c.queryCalls - 1
	if index >= len(c.queryBodies) {
		index = len(c.queryBodies) - 1
	}
	return 200, c.queryBodies[index], nil
}

func (c *fakeRetryClient) RecibirDESync(_ context.Context, _ int64, xml []byte) (int, []byte, error) {
	c.sendCalls++
	c.sentXML = append([]byte(nil), xml...)
	return 200, c.sendBody, nil
}

func newRetryTestDoc(environment, xml string) *tenant.PendingRetryDoc {
	sum := sha256.Sum256([]byte(xml))
	return &tenant.PendingRetryDoc{
		ClientID:     1,
		Environment:  environment,
		CDC:          "01060389648001001000012812026071415704692203",
		NumDocumento: "0000001",
		XMLFirmado:   xml,
		XMLSHA256:    hex.EncodeToString(sum[:]),
	}
}

func TestRetryOneProductionDisabledNeverSends(t *testing.T) {
	t.Parallel()

	failureCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/documents/pending-retry/failure" {
			http.NotFound(w, r)
			return
		}
		failureCalls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{}})
	}))
	defer server.Close()

	w := New(tenant.NewResolver(tenant.NewORDSClient(server.URL, "tok"), make([]byte, 32), 0), Config{MaxRetry: 3})
	factoryCalls := 0
	w.clientFactory = func(context.Context, int, sifen.Environment) (sifenRetryClient, error) {
		factoryCalls++
		return nil, errors.New("must not create a PROD client")
	}
	err := w.retryOne(t.Context(), newRetryTestDoc("PROD", "<rDE/>"), "owner-1")
	if err == nil {
		t.Fatal("retryOne returned nil for disabled PROD")
	}
	if factoryCalls != 0 || failureCalls != 1 {
		t.Fatalf("factoryCalls=%d failureCalls=%d, want 0 and 1", factoryCalls, failureCalls)
	}
}

func TestRetryOneHashMismatchRequiresManualRecovery(t *testing.T) {
	t.Parallel()

	recoveryCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/documents/retry/reconciliation" {
			http.NotFound(w, r)
			return
		}
		recoveryCalls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{}})
	}))
	defer server.Close()

	doc := newRetryTestDoc("TEST", "<rDE/>")
	doc.XMLSHA256 = strings.Repeat("0", 64)
	w := New(tenant.NewResolver(tenant.NewORDSClient(server.URL, "tok"), make([]byte, 32), 0), Config{})
	if err := w.retryOne(t.Context(), doc, "owner-1"); err == nil {
		t.Fatal("retryOne returned nil for XML hash mismatch")
	}
	if recoveryCalls != 1 {
		t.Fatalf("recoveryCalls=%d, want 1", recoveryCalls)
	}
}

func TestRetryOneSendsExactXMLAfterTestQuery(t *testing.T) {
	t.Parallel()

	var registered map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/v1/documents":
			if err := json.NewDecoder(r.Body).Decode(&registered); err != nil {
				t.Errorf("decode registered document: %v", err)
			}
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{}})
	}))
	defer server.Close()

	const xml = "<rDE>bytes exactos</rDE>"
	fake := &fakeRetryClient{
		queryBodies: [][]byte{[]byte(`<rRes><dCodRes>0420</dCodRes></rRes>`)},
		sendBody:    []byte(`<rRes><dCodRes>0200</dCodRes></rRes>`),
	}
	w := New(tenant.NewResolver(tenant.NewORDSClient(server.URL, "tok"), make([]byte, 32), 0), Config{AllowProdWrites: true})
	w.clientFactory = func(context.Context, int, sifen.Environment) (sifenRetryClient, error) { return fake, nil }
	if err := w.retryOne(t.Context(), newRetryTestDoc("TEST", xml), "owner-1"); err != nil {
		t.Fatalf("retryOne: %v", err)
	}
	if fake.queryCalls != 1 || fake.sendCalls != 1 {
		t.Fatalf("queryCalls=%d sendCalls=%d, want 1 and 1", fake.queryCalls, fake.sendCalls)
	}
	if string(fake.sentXML) != xml {
		t.Fatalf("sent XML = %q, want exact %q", fake.sentXML, xml)
	}
	if registered["xml_firmado"] != xml {
		t.Fatalf("persisted XML = %v, want exact %q", registered["xml_firmado"], xml)
	}
}

func TestRetryOneProductionRequiresThree0420Queries(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/documents" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{}})
	}))
	defer server.Close()

	fake := &fakeRetryClient{
		queryBodies: [][]byte{
			[]byte(`<rRes><dCodRes>0420</dCodRes></rRes>`),
			[]byte(`<rRes><dCodRes>0420</dCodRes></rRes>`),
			[]byte(`<rRes><dCodRes>0420</dCodRes></rRes>`),
		},
		sendBody: []byte(`<rRes><dCodRes>0200</dCodRes></rRes>`),
	}
	w := New(tenant.NewResolver(tenant.NewORDSClient(server.URL, "tok"), make([]byte, 32), 0), Config{AllowProdWrites: true})
	var waits []time.Duration
	w.wait = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	w.clientFactory = func(context.Context, int, sifen.Environment) (sifenRetryClient, error) { return fake, nil }
	if err := w.retryOne(t.Context(), newRetryTestDoc("PROD", "<rDE/>"), "owner-1"); err != nil {
		t.Fatalf("retryOne: %v", err)
	}
	if fake.queryCalls != 3 || fake.sendCalls != 1 || len(waits) != 2 || waits[0] != 5*time.Second || waits[1] != 15*time.Second {
		t.Fatalf("queryCalls=%d sendCalls=%d waits=%d, want 3, 1, 2", fake.queryCalls, fake.sendCalls, len(waits))
	}
}

func TestRetryOne0422ConcilesWithoutSending(t *testing.T) {
	t.Parallel()

	reconciled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/documents/reconciliation" {
			http.NotFound(w, r)
			return
		}
		reconciled = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{"estado": "CANCELADO", "cod_res": "0422", "recovery_required": false},
		})
	}))
	defer server.Close()

	fake := &fakeRetryClient{queryBodies: [][]byte{[]byte(`<rRes><dCodRes>0422</dCodRes><rGeVeCan/></rRes>`)}}
	w := New(tenant.NewResolver(tenant.NewORDSClient(server.URL, "tok"), make([]byte, 32), 0), Config{AllowProdWrites: true})
	w.clientFactory = func(context.Context, int, sifen.Environment) (sifenRetryClient, error) { return fake, nil }
	if err := w.retryOne(t.Context(), newRetryTestDoc("TEST", "<rDE/>"), "owner-1"); err != nil {
		t.Fatalf("retryOne: %v", err)
	}
	if !reconciled || fake.sendCalls != 0 {
		t.Fatalf("reconciled=%v sendCalls=%d, want true and 0", reconciled, fake.sendCalls)
	}
}

func TestRetryOneAmbiguousQueryNeverSends(t *testing.T) {
	t.Parallel()

	failureCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/documents/pending-retry/failure" {
			http.NotFound(w, r)
			return
		}
		failureCalls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{}})
	}))
	defer server.Close()

	fake := &fakeRetryClient{queryErr: errors.New("timeout")}
	w := New(tenant.NewResolver(tenant.NewORDSClient(server.URL, "tok"), make([]byte, 32), 0), Config{AllowProdWrites: true})
	w.clientFactory = func(context.Context, int, sifen.Environment) (sifenRetryClient, error) { return fake, nil }
	if err := w.retryOne(t.Context(), newRetryTestDoc("TEST", "<rDE/>"), "owner-1"); err == nil {
		t.Fatal("retryOne returned nil for ambiguous query")
	}
	if fake.sendCalls != 0 || failureCalls != 1 {
		t.Fatalf("sendCalls=%d failureCalls=%d, want 0 and 1", fake.sendCalls, failureCalls)
	}
}
