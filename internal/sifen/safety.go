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

// Environment identifica el ambiente SIFEN. El prefijo de la API key lo determina
// en la capa HTTP (sk_test_ -> EnvTest, sk_prod_ -> EnvProd) y viaja por toda la
// cadena (timbrado/CSC/certificado/URLs) impidiendo mezclar test con producción.
type Environment string

const (
	EnvTest Environment = "test"
	EnvProd Environment = "prod"
)

// ParseEnvironment normaliza un string a Environment.
func ParseEnvironment(s string) (Environment, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "test", "sandbox":
		return EnvTest, nil
	case "prod", "production", "produccion":
		return EnvProd, nil
	default:
		return "", fmt.Errorf("environment inválido: %q (test|prod)", s)
	}
}

// AssertSafeURL valida que la URL corresponda EXACTAMENTE al ambiente indicado.
// Bloquea toda mezcla: una key/ambiente test jamás puede pegarle a producción y
// viceversa. Es el reemplazo generalizado del histórico AssertSafeTestURL.
func AssertSafeURL(raw string, env Environment) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("URL vacía: no se puede validar el ambiente")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("URL inválida %q: %w", raw, err)
	}
	switch env {
	case EnvTest:
		return assertTestURL(u, raw)
	case EnvProd:
		return assertProdURL(u, raw)
	default:
		return fmt.Errorf("environment inválido: %q", env)
	}
}

// AssertSafeTestURL se mantiene por compatibilidad (config.go, CLIs). Delega en
// AssertSafeURL con EnvTest.
func AssertSafeTestURL(raw string) error {
	return AssertSafeURL(raw, EnvTest)
}

// assertTestURL permite solo hosts de prueba: sifen-test o ekuatia consultas-test.
func assertTestURL(u *url.URL, raw string) error {
	host := strings.ToLower(u.Hostname())
	path := strings.ToLower(u.Path)
	full := strings.ToLower(raw)

	if host == HostSifenProd {
		return fmt.Errorf("PROHIBIDO producción: host %s en ambiente test (usá sifen-test)", HostSifenProd)
	}
	if host == HostEkuatia && strings.Contains(full, "/consultas/qr") && !strings.Contains(full, "consultas-test") {
		return fmt.Errorf("PROHIBIDO QR de producción en ambiente test: usá consultas-test")
	}
	if strings.Contains(full, "sifen.set.gov.py") && !strings.Contains(full, "sifen-test") {
		return fmt.Errorf("PROHIBIDO URL de producción SIFEN en ambiente test")
	}

	switch host {
	case HostSifenTest:
		return nil
	case HostEkuatia, "www." + HostEkuatia:
		if strings.Contains(full, "consultas-test") || strings.Contains(path, "consultas-test") {
			return nil
		}
		return fmt.Errorf("ekuatia en ambiente test solo con consultas-test, got %q", raw)
	default:
		return fmt.Errorf("host no permitido en ambiente test: %q (solo sifen-test / consultas-test)", host)
	}
}

// assertProdURL permite solo hosts de producción: sifen.set.gov.py o ekuatia
// consultas (sin el sufijo -test). Bloquea explícitamente los hosts de prueba.
func assertProdURL(u *url.URL, raw string) error {
	host := strings.ToLower(u.Hostname())
	full := strings.ToLower(raw)

	if host == HostSifenTest {
		return fmt.Errorf("PROHIBIDO mezclar: host de prueba %s en ambiente prod", HostSifenTest)
	}
	if strings.Contains(full, "consultas-test") {
		return fmt.Errorf("PROHIBIDO mezclar: QR de prueba (consultas-test) en ambiente prod")
	}

	switch host {
	case HostSifenProd:
		return nil
	case HostEkuatia, "www." + HostEkuatia:
		if strings.Contains(full, "/consultas/") {
			return nil
		}
		return fmt.Errorf("ekuatia en ambiente prod solo con /consultas/ (QR), got %q", raw)
	default:
		return fmt.Errorf("host no permitido en ambiente prod: %q (solo sifen.set.gov.py / consultas)", host)
	}
}
