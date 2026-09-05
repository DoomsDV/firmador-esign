package webhook

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidateWebhookURL aplica controles básicos anti-SSRF para URLs configuradas por el tenant.
func ValidateWebhookURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("url vacía")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("url inválida: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("solo se permite HTTPS")
	}
	if u.Host == "" {
		return fmt.Errorf("host requerido")
	}
	host := strings.ToLower(strings.Split(u.Hostname(), ":")[0])
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("host no permitido")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("ip no permitida")
		}
	}
	return nil
}

func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip.IsPrivate() {
		return true
	}
	// Metadata clouds comunes
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 169 && v4[1] == 254 {
			return true
		}
	}
	return false
}
