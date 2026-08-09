// Package gotenberg es un cliente HTTP mínimo del servicio Gotenberg (Chromium
// headless en Docker) usado para convertir el HTML del KuDE a PDF. Sin reintentos:
// la generación del KuDE es best-effort y asíncrona, nunca bloquea la emisión SIFEN.
package gotenberg

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// DefaultTimeout es el timeout por defecto de la llamada a Gotenberg si el llamador
// no especifica uno propio en el Client.
const DefaultTimeout = 20 * time.Second

// Client encapsula la URL base de Gotenberg y el http.Client usado para llamarlo.
type Client struct {
	baseURL string
	http    *http.Client
}

// New crea un cliente contra baseURL (p. ej. http://localhost:3000). timeout <= 0
// usa DefaultTimeout.
func New(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		http:    &http.Client{Timeout: timeout},
	}
}

// Render convierte el HTML dado a PDF vía POST {baseURL}/forms/chromium/convert/html.
// Gotenberg requiere que el archivo principal se llame "index.html".
func (c *Client) Render(ctx context.Context, html string) ([]byte, error) {
	if c == nil || c.baseURL == "" {
		return nil, fmt.Errorf("gotenberg: cliente no configurado (GOTENBERG_URL vacío)")
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("files", "index.html")
	if err != nil {
		return nil, fmt.Errorf("gotenberg: crear form-file: %w", err)
	}
	if _, err := part.Write([]byte(html)); err != nil {
		return nil, fmt.Errorf("gotenberg: escribir html: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("gotenberg: cerrar multipart: %w", err)
	}

	url := c.baseURL + "/forms/chromium/convert/html"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return nil, fmt.Errorf("gotenberg: crear request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gotenberg: request a %s: %w", url, err)
	}
	defer resp.Body.Close()

	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gotenberg: leer respuesta: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		snippet := out
		if len(snippet) > 500 {
			snippet = snippet[:500]
		}
		return nil, fmt.Errorf("gotenberg: HTTP %d: %s", resp.StatusCode, snippet)
	}
	return out, nil
}
