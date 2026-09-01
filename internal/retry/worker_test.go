package retry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
