package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseCORSOrigins(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{
			name: "empty uses defaults",
			raw:  "",
			want: defaultCORSOrigins,
		},
		{
			name: "comma separated",
			raw:  "https://a.example, https://b.example",
			want: []string{"https://a.example", "https://b.example"},
		},
		{
			name: "skips empty parts",
			raw:  "https://a.example,,",
			want: []string{"https://a.example"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseCORSOrigins(tt.raw)
			if len(got) != len(tt.want) {
				t.Fatalf("len(got)=%d want %d", len(got), len(tt.want))
			}
			for _, o := range tt.want {
				if _, ok := got[o]; !ok {
					t.Fatalf("missing origin %q in %v", o, got)
				}
			}
		})
	}
}

func TestCorsMiddleware(t *testing.T) {
	t.Parallel()
	allowed := parseCORSOrigins("https://staging.etick.uno,https://etick.uno")
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	tests := []struct {
		name           string
		method         string
		origin         string
		wantStatus     int
		wantACAO       string
		wantInnerHit   bool
	}{
		{
			name:         "OPTIONS allowed origin",
			method:       http.MethodOptions,
			origin:       "https://staging.etick.uno",
			wantStatus:   http.StatusNoContent,
			wantACAO:     "https://staging.etick.uno",
			wantInnerHit: false,
		},
		{
			name:         "OPTIONS disallowed origin",
			method:       http.MethodOptions,
			origin:       "https://evil.example",
			wantStatus:   http.StatusNoContent,
			wantACAO:     "",
			wantInnerHit: false,
		},
		{
			name:         "PUT allowed origin passes through",
			method:       http.MethodPut,
			origin:       "https://staging.etick.uno",
			wantStatus:   http.StatusTeapot,
			wantACAO:     "https://staging.etick.uno",
			wantInnerHit: true,
		},
		{
			name:         "GET without origin passes through",
			method:       http.MethodGet,
			origin:       "",
			wantStatus:   http.StatusTeapot,
			wantACAO:     "",
			wantInnerHit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hit := false
			handler := corsMiddleware(allowed)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hit = true
				inner.ServeHTTP(w, r)
			}))

			req := httptest.NewRequest(tt.method, "/v1/panel/environments", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status: got %d want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != tt.wantACAO {
				t.Errorf("Access-Control-Allow-Origin: got %q want %q", got, tt.wantACAO)
			}
			if hit != tt.wantInnerHit {
				t.Errorf("inner handler hit: got %v want %v", hit, tt.wantInnerHit)
			}
			if tt.wantACAO != "" {
				if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
					t.Error("expected Access-Control-Allow-Methods")
				}
				if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
					t.Error("expected Access-Control-Allow-Headers")
				}
			}
		})
	}
}
