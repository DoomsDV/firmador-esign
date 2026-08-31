package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/DoomsDV/firmador-e/internal/kude"
	"github.com/DoomsDV/firmador-e/internal/sifen"
	"github.com/DoomsDV/firmador-e/internal/tenant"
)

const (
	persistImmediateAttempts = 3
	persistImmediateBackoff  = 200 * time.Millisecond
)

// handleCreateDocument construye, firma y envía un DE a SIFEN (test/prod según la
// key), luego persiste el resultado en ORDS.
func (s *Server) handleCreateDocument(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := tenantFromContext(ctx)

	var req createDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "body inválido: "+err.Error())
		return
	}
	if !cfg.CertAvailable {
		writeErr(w, http.StatusUnprocessableEntity, "NO_CERTIFICATE", "el tenant no tiene certificado cargado")
		return
	}

	idemKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	op, err := resolveOperacion(cfg, &req)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "INVALID_OPERATION", err.Error())
		return
	}

	claimed := false
	sendStarted := false
	if idemKey != "" {
		claim, err := s.resolver.ORDS().ClaimIdempotency(ctx, cfg.ClientID, cfg.EnvUpper(), idemKey)
		if err != nil {
			writeErr(w, http.StatusBadGateway, "IDEMPOTENCY_ERROR", "no se pudo reclamar Idempotency-Key: "+err.Error())
			return
		}
		switch claim.ClaimStatus {
		case "COMPLETED":
			if claim.CDC == "" {
				writeErr(w, http.StatusBadGateway, "IDEMPOTENCY_ERROR", "Idempotency-Key completada sin documento")
				return
			}
			writeIdempotentDocument(w, claim)
			return
		case "IN_FLIGHT":
			writeErr(w, http.StatusConflict, "IDEMPOTENCY_IN_PROGRESS",
				"ya hay una solicitud en curso con esta Idempotency-Key; reintentá con la misma clave")
			return
		case "ACQUIRED":
			claimed = true
		default:
			writeErr(w, http.StatusBadGateway, "IDEMPOTENCY_ERROR", "respuesta de reclamo de idempotencia inválida")
			return
		}
	}
	defer func() {
		if !claimed || sendStarted {
			return
		}
		releaseCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.resolver.ORDS().ReleaseIdempotency(releaseCtx, cfg.ClientID, cfg.EnvUpper(), idemKey); err != nil {
			log.Printf("[warn] release idempotency key for client %d: %v", cfg.ClientID, err)
		}
	}()

	numeroDoc, err := s.resolver.ORDS().NextNumber(ctx, cfg.ClientID, cfg.EnvUpper(), op.Est.Codigo, op.Punto, op.TipoDE)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "SEQUENCE_ERROR", "no se pudo obtener el número correlativo: "+err.Error())
		return
	}

	build, err := buildDocumentInput(cfg, &req, op, numeroDoc, s.fechaFirma())
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "INVALID_DOCUMENT", err.Error())
		return
	}

	rde, err := sifen.BuildDE(build.Input)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "BUILD_ERROR", err.Error())
		return
	}

	cert, err := s.resolver.GetCertificate(ctx, cfg)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "CERT_FETCH_ERROR", err.Error())
		return
	}
	digital, err := sifen.LoadCertificateFromBytes(cert.P12, cert.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "CERT_DECODE_ERROR", err.Error())
		return
	}
	csc, err := s.resolver.GetCSC(ctx, cfg)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "CSC_FETCH_ERROR", err.Error())
		return
	}

	cdc := rde.DE.Id
	feEmi := rde.DE.GDatGralOpe.DFeEmiDE

	var totGralOpe, totIVA decimal.Decimal
	if rde.DE.GTotSub != nil {
		totGralOpe = rde.DE.GTotSub.DTotGralOpe
		totIVA = rde.DE.GTotSub.DTotIVA
	}

	var qrURL string
	_, xmlOut, err := sifen.FirmarYSerializar(rde, digital, func(dv string) (string, error) {
		u, err := sifen.BuildCarQR(sifen.QRParams{
			CDC:         cdc,
			FeEmiDE:     feEmi,
			RecField:    sifen.ReceptorFieldForQR(rde.DE.GDatGralOpe.GDatRec),
			RecID:       sifen.ReceptorIDForQR(rde.DE.GDatGralOpe.GDatRec),
			TotGralOpe:  sifen.DecimalPlain(totGralOpe),
			TotIVA:      sifen.DecimalPlain(totIVA),
			Items:       len(rde.DE.GDtipDE.GCamItem),
			DigestValue: dv,
			IdCSC:       cfg.Timbrado.IdCSC,
			CSC:         csc,
			Env:         cfg.Environment,
		})
		qrURL = u
		return u, err
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "SIGN_ERROR", "error al firmar: "+err.Error())
		return
	}

	// Envío síncrono a SIFEN.
	syncURL := sifen.EndpointsFor(cfg.Environment).WsSync
	client, err := sifen.NewClientFromP12Bytes(cfg.Environment, cert.P12, cert.Password, syncURL)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "WS_CLIENT_ERROR", err.Error())
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	sendStarted = true
	code, body, err := client.RecibirDESync(sendCtx, int64(numeroDoc), xmlOut)
	if err != nil {
		// El DE quedó firmado pero no se pudo enviar: se registra como FIRMADO.
		s.persistDocument(ctx, cfg, build, cdc, numeroDoc, totGralOpe, qrURL, xmlOut, sifenResult{}, "FIRMADO", idemKey)
		writeErr(w, http.StatusBadGateway, "SEND_ERROR", "firmado pero no enviado a SIFEN: "+err.Error())
		return
	}

	res := parseSifenResult(body)
	estado := estadoDE(res.CodRes)
	s.persistDocument(ctx, cfg, build, cdc, numeroDoc, totGralOpe, qrURL, xmlOut, res, estado, idemKey)

	if estado == "APROBADO" {
		s.enqueueKudeDurable(cfg, rde, qrURL, cdc)
	}

	status := http.StatusCreated
	if estado != "APROBADO" {
		status = http.StatusOK // rechazado por SIFEN: 200 con el detalle en data
	}
	_ = code
	writeOK(w, status, documentResponse{
		CDC:             cdc,
		Estado:          estado,
		CodRes:          res.CodRes,
		ProtAut:         res.ProtAut,
		Mensaje:         res.MsgRes,
		QR:              qrURL,
		NumeroDocumento: fmt.Sprintf("%07d", numeroDoc),
		Ambiente:        string(cfg.Environment),
	})
}

// writeIdempotentDocument responde el resultado previamente persistido de una
// Idempotency-Key completada, sin construir, firmar ni reenviar el DE.
func writeIdempotentDocument(w http.ResponseWriter, doc *tenant.IdempotencyClaim) {
	writeOK(w, http.StatusOK, documentResponse{
		CDC:             doc.CDC,
		Estado:          doc.Estado,
		CodRes:          doc.CodRes,
		ProtAut:         doc.ProtAut,
		Mensaje:         doc.MensajeRes,
		QR:              doc.QRURL,
		NumeroDocumento: doc.NumeroDocumento,
		Ambiente:        doc.Ambiente,
	})
}

// persistDocument registra el documento y su XML/QR en ORDS con reintentos
// inmediatos. Si falla tras los reintentos, deja señal de recovery (log CRITICAL
// + reintento en background) sin invalidar la emisión ya realizada.
func (s *Server) persistDocument(ctx context.Context, cfg *tenant.Config, build *buildResult, cdc string, numeroDoc int, total decimal.Decimal, qrURL string, xmlOut []byte, res sifenResult, estado, idemKey string) {
	sum := sha256.Sum256(xmlOut)
	rec := tenant.DocumentRecord{
		ClientID:        cfg.ClientID,
		Environment:     cfg.EnvUpper(),
		TipoDE:          build.TipoDE,
		CDC:             cdc,
		NumeroDoc:       fmt.Sprintf("%07d", numeroDoc),
		Establecimiento: build.Establecimiento,
		PuntoExpedicion: build.Punto,
		ReceptorNombre:  build.ReceptorNombre,
		ReceptorDoc:     build.ReceptorDoc,
		Moneda:          build.Input.Moneda,
		TotalOperacion:  sifen.DecimalPlain(total),
		Estado:          estado,
		CodRes:          res.CodRes,
		ProtAut:         res.ProtAut,
		MensajeRes:      res.MsgRes,
		XMLFirmado:      string(xmlOut),
		XMLSHA256:       hex.EncodeToString(sum[:]),
		XMLSizeBytes:    len(xmlOut),
		XMLMimeType:     "application/xml; charset=UTF-8",
		QRURL:           qrURL,
		IdempotencyKey:  idemKey,
	}

	var lastErr error
	for attempt := 1; attempt <= persistImmediateAttempts; attempt++ {
		err := s.resolver.ORDS().RegisterDocument(ctx, rec)
		if err == nil {
			return
		}
		lastErr = err
		log.Printf("[error] register document cdc=%s attempt=%d/%d: %v", cdc, attempt, persistImmediateAttempts, err)
		if attempt < persistImmediateAttempts {
			time.Sleep(persistImmediateBackoff * time.Duration(attempt))
		}
	}

	log.Printf("[CRITICAL] persist document FAILED cdc=%s client=%d estado=%s after %d attempts: %v — recovery needed",
		cdc, cfg.ClientID, estado, persistImmediateAttempts, lastErr)
	_ = s.resolver.ORDS().Log(context.Background(), tenant.LogEntry{
		ClientID:    cfg.ClientID,
		Environment: cfg.EnvUpper(),
		Endpoint:    "persist_document_failed:" + cdc,
		HTTPStatus:  500,
		LatencyMS:   0,
	})

	// Recovery durable: reintentos en background con backoff más largo.
	go s.recoverPersistDocument(rec)
}

func (s *Server) recoverPersistDocument(rec tenant.DocumentRecord) {
	const maxBg = 8
	backoff := time.Second
	for attempt := 1; attempt <= maxBg; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := s.resolver.ORDS().RegisterDocument(ctx, rec)
		cancel()
		if err == nil {
			log.Printf("[info] persist recovery OK cdc=%s attempt=%d", rec.CDC, attempt)
			return
		}
		log.Printf("[error] persist recovery cdc=%s attempt=%d/%d: %v", rec.CDC, attempt, maxBg, err)
		time.Sleep(backoff)
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
	log.Printf("[CRITICAL] persist recovery EXHAUSTED cdc=%s — intervención manual requerida", rec.CDC)
}

// handleGetDocumentXML sirve el XML firmado canónico (bytes exactos del CLOB)
// autenticado con API key. Solo del client_id de la key y solo si APROBADO.
func (s *Server) handleGetDocumentXML(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := tenantFromContext(ctx)
	cdc := r.PathValue("cdc")

	art, err := s.resolver.ORDS().GetXMLArtifact(ctx, cfg.ClientID, cdc)
	if err != nil {
		var ordsErr *tenant.ORDSError
		if errors.As(err, &ordsErr) {
			switch {
			case ordsErr.HTTPStatus == http.StatusNotFound || ordsErr.Code == "NOT_FOUND":
				writeErr(w, http.StatusNotFound, "NOT_FOUND", "XML inexistente")
				return
			case ordsErr.HTTPStatus == http.StatusConflict || ordsErr.Code == "CONFLICT" ||
				strings.Contains(strings.ToUpper(ordsErr.Message), "NO APROBADO"):
				writeErr(w, http.StatusConflict, "NOT_APPROVED", "documento no APROBADO")
				return
			}
		}
		writeErr(w, http.StatusBadGateway, "ORDS_ERROR", err.Error())
		return
	}
	if art.XMLFirmado == "" {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "XML inexistente")
		return
	}

	body := []byte(art.XMLFirmado)
	sum := sha256.Sum256(body)
	if art.CDC != cdc || art.Estado != "APROBADO" ||
		art.XMLAvailability != "AVAILABLE" ||
		art.XMLSHA256 == "" ||
		!strings.EqualFold(hex.EncodeToString(sum[:]), art.XMLSHA256) ||
		art.XMLSizeBytes != len(body) {
		writeErr(w, http.StatusBadGateway, "XML_INTEGRITY_ERROR",
			"el artefacto XML almacenado no supera la verificación de integridad")
		return
	}

	mime := art.XMLMimeType
	if mime == "" {
		mime = "application/xml; charset=UTF-8"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if art.XMLSHA256 != "" {
		w.Header().Set("X-Content-SHA256", art.XMLSHA256)
	}
	w.Header().Set("X-CDC", art.CDC)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// handleCancelDocument emite un evento de cancelación sobre un CDC.
func (s *Server) handleCancelDocument(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := tenantFromContext(ctx)
	cdc := r.PathValue("cdc")

	var req cancelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "body inválido: "+err.Error())
		return
	}

	ev, err := sifen.NewEventoCancelacion(eventID(), cdc, req.Motivo, s.fechaFirma())
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "INVALID_EVENT", err.Error())
		return
	}

	res, err := s.sendEvento(ctx, cfg, ev)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "SEND_ERROR", err.Error())
		return
	}
	estado := estadoEvento(res.CodRes)

	_ = s.resolver.ORDS().RegisterEvent(ctx, tenant.EventRecord{
		ClientID:   cfg.ClientID,
		CDC:        cdc,
		TipoEvento: "CANCELACION",
		Estado:     estado,
		CodRes:     res.CodRes,
		ProtAut:    res.ProtAut,
		Motivo:     req.Motivo,
	})

	writeOK(w, http.StatusOK, eventResponse{
		Estado:   estado,
		CodRes:   res.CodRes,
		ProtAut:  res.ProtAut,
		Mensaje:  res.MsgRes,
		Ambiente: string(cfg.Environment),
	})
}

// handleGetKude consulta la URL pública del KuDE (PDF) de un documento ya
// emitido. La generación es asíncrona vía cola durable; puede devolver
// "pending"/"failed" antes de tener kudeUrl.
func (s *Server) handleGetKude(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := tenantFromContext(ctx)
	cdc := r.PathValue("cdc")

	kudeURL, estado, err := s.resolver.ORDS().GetKude(ctx, cfg.ClientID, cdc)
	if err != nil {
		var ordsErr *tenant.ORDSError
		if errors.As(err, &ordsErr) && ordsErr.HTTPStatus == http.StatusNotFound {
			writeErr(w, http.StatusNotFound, "NOT_FOUND", "documento inexistente")
			return
		}
		writeErr(w, http.StatusBadGateway, "ORDS_ERROR", err.Error())
		return
	}

	writeOK(w, http.StatusOK, kudeResponse{
		CDC:     cdc,
		Estado:  estado,
		KudeURL: kudeURL,
	})
}

// handleInutilizacion inutiliza un rango de numeración.
func (s *Server) handleInutilizacion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := tenantFromContext(ctx)

	var req inutilizacionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "body inválido: "+err.Error())
		return
	}
	if cfg.Timbrado.NumTimbrado == "" {
		writeErr(w, http.StatusUnprocessableEntity, "NO_TIMBRADO", "el tenant no tiene timbrado en el ambiente")
		return
	}

	est, err := cfg.SelectEstablecimiento(req.Establecimiento)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "INVALID_OPERATION", err.Error())
		return
	}
	punto, err := est.SelectPunto(req.PuntoExpedicion)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "INVALID_OPERATION", err.Error())
		return
	}

	ev, err := sifen.NewEventoInutilizacion(eventID(), cfg.Timbrado.NumTimbrado, est.Codigo, punto,
		req.NumeroInicial, req.NumeroFinal, req.TipoDE, req.Motivo, s.fechaFirma())
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "INVALID_EVENT", err.Error())
		return
	}

	res, err := s.sendEvento(ctx, cfg, ev)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "SEND_ERROR", err.Error())
		return
	}
	estado := estadoEvento(res.CodRes)

	_ = s.resolver.ORDS().RegisterEvent(ctx, tenant.EventRecord{
		ClientID:   cfg.ClientID,
		TipoEvento: "INUTILIZACION",
		Estado:     estado,
		CodRes:     res.CodRes,
		ProtAut:    res.ProtAut,
		Motivo:     req.Motivo,
	})

	writeOK(w, http.StatusOK, eventResponse{
		Estado:   estado,
		CodRes:   res.CodRes,
		ProtAut:  res.ProtAut,
		Mensaje:  res.MsgRes,
		Ambiente: string(cfg.Environment),
	})
}

// sendEvento firma y envía un evento al WS de eventos del ambiente.
func (s *Server) sendEvento(ctx context.Context, cfg *tenant.Config, ev *sifen.REve) (sifenResult, error) {
	cert, err := s.resolver.GetCertificate(ctx, cfg)
	if err != nil {
		return sifenResult{}, err
	}
	digital, err := sifen.LoadCertificateFromBytes(cert.P12, cert.Password)
	if err != nil {
		return sifenResult{}, err
	}
	firmado, err := sifen.FirmarEvento(ev, digital)
	if err != nil {
		return sifenResult{}, err
	}

	eventoURL := sifen.EndpointsFor(cfg.Environment).WsEvento
	client, err := sifen.NewClientFromP12Bytes(cfg.Environment, cert.P12, cert.Password, sifen.EndpointsFor(cfg.Environment).WsSync)
	if err != nil {
		return sifenResult{}, err
	}
	sendCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	id, _ := strconv.Atoi(ev.Id)
	_, body, err := client.RecibirEventoSync(sendCtx, int64(id), eventoURL, firmado)
	if err != nil {
		return sifenResult{}, err
	}
	return parseSifenResult(body), nil
}

// eventID genera un id de evento único por segundo (suficiente para el dId).
func eventID() int {
	return int(time.Now().Unix() % 1000000000)
}

// enqueueKudeDurable arma KudeData en la goroutine de la request (rde no es
// concurrency-safe) y encola la tarea durable. El worker kudequeue renderiza
// y sube el PDF. Best-effort respecto de la respuesta HTTP ya enviada.
func (s *Server) enqueueKudeDurable(cfg *tenant.Config, rde *sifen.RDE, qrURL, cdc string) {
	data, err := kude.BuildKudeData(rde, qrURL, cfg.EnvUpper(), kude.Branding{
		TemplateID:      cfg.KudeConfig.TemplateID,
		ColorPrimario:   cfg.KudeConfig.ColorPrimario,
		LogoURL:         cfg.KudeConfig.LogoURL,
		NotasFooter:     cfg.KudeConfig.NotasFooter,
		MostrarFantasia: cfg.KudeConfig.MostrarFantasia,
	})
	if err != nil {
		log.Printf("[warn] kude enqueue %s: armando datos: %v", cdc, err)
		return
	}
	payload, err := json.Marshal(data)
	if err != nil {
		log.Printf("[warn] kude enqueue %s: serializar payload: %v", cdc, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.resolver.ORDS().EnqueueKudeTask(ctx, cfg.ClientID, cdc, string(payload)); err != nil {
		log.Printf("[warn] kude enqueue %s: ORDS: %v", cdc, err)
	}
}
