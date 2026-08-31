package tenant

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
)

func TestClaimIdempotencyConcurrentRequestsHaveSingleOwner(t *testing.T) {
	t.Parallel()

	type claimRequest struct {
		ClientID       int    `json:"client_id"`
		Environment    string `json:"environment"`
		IdempotencyKey string `json:"idempotency_key"`
	}

	var mu sync.Mutex
	claims := make(map[string]struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/documents/idempotency/claim" {
			http.NotFound(w, r)
			return
		}
		var request claimRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		key := strconv.Itoa(request.ClientID) + "/" + request.Environment + "/" + request.IdempotencyKey
		mu.Lock()
		_, exists := claims[key]
		if !exists {
			claims[key] = struct{}{}
		}
		mu.Unlock()

		status := "ACQUIRED"
		if exists {
			status = "IN_FLIGHT"
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"claim_status": status,
				"found":        false,
			},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := NewORDSClient(server.URL, "test-token")
	const callers = 32
	results := make(chan string, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claim, err := client.ClaimIdempotency(t.Context(), 42, "TEST", "invoice-123")
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
	if acquired != 1 {
		t.Errorf("acquired=%d want 1", acquired)
	}
	if inFlight != callers-1 {
		t.Errorf("in_flight=%d want %d", inFlight, callers-1)
	}
}
