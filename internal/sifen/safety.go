package sifen

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	HostSifenTest = "sifen-test.set.gov.py"
	HostSifenProd = "sifen.set.gov.py"
	HostEkuatia   = "ekuatia.set.gov.py"
)

// AssertSafeTestURL aborta uso de producción: solo sifen-test o ekuatia consultas-test.
func AssertSafeTestURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("URL vacía: solo se permiten hosts de prueba")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("URL inválida %q: %w", raw, err)
	}

	host := strings.ToLower(u.Hostname())
	path := strings.ToLower(u.Path)
	full := strings.ToLower(raw)

	// Deny producción explícita
	if host == HostSifenProd {
		return fmt.Errorf("PROHIBIDO producción: host %s (usá sifen-test)", HostSifenProd)
	}
	if host == HostEkuatia && strings.Contains(full, "/consultas/qr") && !strings.Contains(full, "consultas-test") {
		return fmt.Errorf("PROHIBIDO QR de producción: usá consultas-test")
	}
	if strings.Contains(full, "sifen.set.gov.py") && !strings.Contains(full, "sifen-test") {
		return fmt.Errorf("PROHIBIDO URL de producción SIFEN")
	}

	// Allow list
	switch host {
	case HostSifenTest:
		return nil
	case HostEkuatia:
		if strings.Contains(full, "consultas-test") || strings.Contains(path, "consultas-test") {
			return nil
		}
		return fmt.Errorf("ekuatia solo permitido con consultas-test, got %q", raw)
	case "www.ekuatia.set.gov.py":
		if strings.Contains(full, "consultas-test") {
			return nil
		}
		return fmt.Errorf("ekuatia solo permitido con consultas-test, got %q", raw)
	default:
		return fmt.Errorf("host no permitido en fase test: %q (solo sifen-test / consultas-test)", host)
	}
}
