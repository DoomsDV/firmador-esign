package tenant

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterDocumentIncludesXMLAndHash(t *testing.T) {
	t.Parallel()

	xmlBody := `<?xml version="1.0"?><rDE><DE Id="CDC123"/></rDE>`
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/documents" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    map[string]any{"document_id": 1, "cdc": "CDC123"},
		})
	}))
	defer server.Close()

	client := NewORDSClient(server.URL, "tok")
	err := client.RegisterDocument(t.Context(), DocumentRecord{
		ClientID:    7,
		Environment: "TEST",
		CDC:         "CDC123",
		Estado:      "APROBADO",
		XMLFirmado:  xmlBody,
		QRURL:       "https://example.test/qr",
	})
	if err != nil {
		t.Fatalf("RegisterDocument: %v", err)
	}
	if got["xml_firmado"] != xmlBody {
		t.Fatalf("xml_firmado = %v, want exact signed XML", got["xml_firmado"])
	}
	sum := sha256.Sum256([]byte(xmlBody))
	wantHash := hex.EncodeToString(sum[:])
	if got["xml_sha256"] != wantHash {
		t.Fatalf("xml_sha256 = %v, want %s", got["xml_sha256"], wantHash)
	}
	size, ok := got["xml_size_bytes"].(float64)
	if !ok || int(size) != len(xmlBody) {
		t.Fatalf("xml_size_bytes = %v, want %d", got["xml_size_bytes"], len(xmlBody))
	}
}

func TestEnqueueKudeTaskPayload(t *testing.T) {
	t.Parallel()

	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/kude-task/enqueue") {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"status": "PENDING"}})
	}))
	defer server.Close()

	client := NewORDSClient(server.URL, "tok")
	payload := `{"CDC":"CDC1","QRURL":"https://qr"}`
	if err := client.EnqueueKudeTask(t.Context(), 3, "CDC1", payload); err != nil {
		t.Fatalf("EnqueueKudeTask: %v", err)
	}
	if got["client_id"].(float64) != 3 {
		t.Fatalf("client_id = %v", got["client_id"])
	}
	if got["cdc"] != "CDC1" {
		t.Fatalf("cdc = %v", got["cdc"])
	}
	if got["payload_json"] != payload {
		t.Fatalf("payload_json = %v", got["payload_json"])
	}
}

func TestListKudeRecoveryDocuments(t *testing.T) {
	t.Parallel()

	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/documents/kude-recovery" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": []map[string]any{{
				"client_id":   7,
				"cdc":         "CDC123",
				"environment": "TEST",
				"xml_firmado": "<rDE/>",
				"qr_url":      "https://example.test/qr",
			}},
		})
	}))
	defer server.Close()

	client := NewORDSClient(server.URL, "tok")
	docs, err := client.ListKudeRecoveryDocuments(t.Context(), 3)
	if err != nil {
		t.Fatalf("ListKudeRecoveryDocuments: %v", err)
	}
	if got["limit"] != float64(3) {
		t.Fatalf("limit = %v, want 3", got["limit"])
	}
	if len(docs) != 1 || docs[0].CDC != "CDC123" || docs[0].ClientID != 7 {
		t.Fatalf("docs = %#v, want one recovery document", docs)
	}
}

func TestListPendingRetrySendsServiceTokenAndPropagatesUnauthorized(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/documents/pending-retry" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("X-Service-Token"); got != "expected-token" {
			t.Errorf("X-Service-Token = %q, want %q", got, "expected-token")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error": map[string]string{
				"code":    "UNAUTHORIZED",
				"message": "service token invalido",
			},
		})
	}))
	defer server.Close()

	client := NewORDSClient(server.URL, "expected-token")
	_, err := client.ListPendingRetry(t.Context(), "all", 1)
	if err == nil {
		t.Fatal("ListPendingRetry returned nil error for HTTP 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("ListPendingRetry error = %q, want HTTP 401 context", err)
	}
}
