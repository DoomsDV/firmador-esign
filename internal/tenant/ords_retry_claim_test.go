package tenant

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestClaimPendingRetrySendsProductionPolicy(t *testing.T) {
	t.Parallel()

	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/documents/pending-retry/claim" {
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
				"client_id": 7, "environment": "TEST", "cdc": "CDC1",
				"xml_firmado": "<rDE/>", "xml_sha256": "abc",
			}},
		})
	}))
	defer server.Close()

	client := NewORDSClient(server.URL, "tok")
	docs, err := client.ClaimPendingRetry(t.Context(), 25, "worker-1", 180, 10, true)
	if err != nil {
		t.Fatalf("ClaimPendingRetry: %v", err)
	}
	if got["limit"] != float64(25) || got["owner"] != "worker-1" {
		t.Fatalf("claim request = %#v", got)
	}
	if got["allow_prod"] != float64(1) {
		t.Fatalf("allow_prod = %v, want numeric 1", got["allow_prod"])
	}
	if len(docs) != 1 || docs[0].XMLFirmado != "<rDE/>" || docs[0].XMLSHA256 != "abc" {
		t.Fatalf("claimed docs = %#v", docs)
	}
}

func TestClaimEventIdempotencyMapsClaimResponse(t *testing.T) {
	t.Parallel()

	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/events/idempotency/claim" {
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
			"data": map[string]any{
				"claim_status": "ACQUIRED", "found": false,
			},
		})
	}))
	defer server.Close()

	client := NewORDSClient(server.URL, "tok")
	claim, err := client.ClaimEventIdempotency(t.Context(), 7, "TEST", "CDC1", "123", "CANCELACION", "key-1", "motivo", "sha")
	if err != nil {
		t.Fatalf("ClaimEventIdempotency: %v", err)
	}
	for field, want := range map[string]any{
		"client_id": 7, "environment": "TEST", "cdc": "CDC1", "event_id": "123",
		"tipo_evento": "CANCELACION", "idempotency_key": "key-1", "motivo": "motivo",
		"request_sha256": "sha",
	} {
		if got[field] != want {
			t.Errorf("%s = %v, want %v", field, got[field], want)
		}
	}
	if claim.ClaimStatus != "ACQUIRED" {
		t.Fatalf("claim status = %q, want ACQUIRED", claim.ClaimStatus)
	}
}

func TestClaimEventIdempotencyConcurrentRequestsHaveSingleOwner(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	claimed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/events/idempotency/claim" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		status := "ACQUIRED"
		if claimed {
			status = "IN_FLIGHT"
		} else {
			claimed = true
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{"claim_status": status, "found": false},
		})
	}))
	defer server.Close()

	client := NewORDSClient(server.URL, "tok")
	const callers = 24
	results := make(chan string, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claim, err := client.ClaimEventIdempotency(t.Context(), 7, "TEST", "CDC1", "123", "CANCELACION", "key-1", "motivo", "sha")
			if err != nil {
				errs <- err
				return
			}
			results <- claim.ClaimStatus
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Errorf("claim: %v", err)
	}
	var acquired, inFlight int
	for result := range results {
		switch result {
		case "ACQUIRED":
			acquired++
		case "IN_FLIGHT":
			inFlight++
		default:
			t.Errorf("unexpected claim status %q", result)
		}
	}
	if acquired != 1 || inFlight != callers-1 {
		t.Fatalf("acquired=%d in_flight=%d, want 1 and %d", acquired, inFlight, callers-1)
	}
}
