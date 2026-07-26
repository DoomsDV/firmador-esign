package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/DoomsDV/firmador-e/internal/tenant"
)

// logInfo es un contenedor mutable que el middleware de auth completa con el tenant
// resuelto para que withLogging (mas externo) lo lea al terminar el request. Se pasa
// por puntero via contexto: la cadena de middlewares no propaga hacia afuera los
// cambios de contexto de las capas internas, pero si las mutaciones de un puntero.
type logInfo struct {
	clientID    int
	environment string
}

// statusRecorder captura el codigo HTTP escrito por el handler (por defecto 200 si
// el handler no llama a WriteHeader explicitamente antes de escribir el cuerpo).
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.status = http.StatusOK
		r.wroteHeader = true
	}
	return r.ResponseWriter.Write(b)
}

// withLogging envuelve un handler para registrar cada request en api_log via el
// endpoint interno de ORDS. Es best-effort y asincrono: nunca bloquea ni altera la
// respuesta al cliente. Debe ser el middleware MAS EXTERNO de la cadena.
func (s *Server) withLogging(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		info := &logInfo{}
		ctx := context.WithValue(r.Context(), ctxKeyLog, info)
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next(rec, r.WithContext(ctx))

		entry := tenant.LogEntry{
			Endpoint:    r.Method + " " + r.URL.Path,
			HTTPStatus:  rec.status,
			LatencyMS:   time.Since(start).Milliseconds(),
			ClientID:    info.clientID,
			Environment: info.environment,
		}
		// El registro no debe bloquear la respuesta ni fallar el request: se hace en
		// una goroutine con su propio contexto (el del request ya pudo cancelarse).
		go func() {
			logCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = s.resolver.ORDS().Log(logCtx, entry)
		}()
	}
}

// logInfoFromContext recupera el contenedor de log (o nil si no se inicio logging).
func logInfoFromContext(ctx context.Context) *logInfo {
	info, _ := ctx.Value(ctxKeyLog).(*logInfo)
	return info
}
