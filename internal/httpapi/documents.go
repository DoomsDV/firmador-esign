package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	"github.com/DoomsDV/firmador-e/internal/kude"
	"github.com/DoomsDV/firmador-e/internal/sifen"
	"github.com/DoomsDV/firmador-e/internal/tenant"
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

	op, err := resolveOperacion(cfg, &req)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "INVALID_OPERATION", err.Error())
		return
	}

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
	code, body, err := client.RecibirDESync(sendCtx, int64(numeroDoc), xmlOut)
	if err != nil {
		// El DE quedó firmado pero no se pudo enviar: se registra como FIRMADO.
		s.persistDocument(ctx, cfg, build, cdc, numeroDoc, totGralOpe, qrURL, xmlOut, sifenResult{}, "FIRMADO")
		writeErr(w, http.StatusBadGateway, "SEND_ERROR", "firmado pero no enviado a SIFEN: "+err.Error())
		return
	}

	res := parseSifenResult(body)
	estado := estadoDE(res.CodRes)
	s.persistDocument(ctx, cfg, build, cdc, numeroDoc, totGralOpe, qrURL, xmlOut, res, estado)

	if estado == "APROBADO" {
		s.triggerKudeGeneration(cfg, rde, qrURL, cdc)
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

// persistDocument registra el documento y su XML/QR en ORDS (best-effort: un fallo
// de persistencia no invalida la emisión ya realizada).
func (s *Server) persistDocument(ctx context.Context, cfg *tenant.Config, build *buildResult, cdc string, numeroDoc int, total decimal.Decimal, qrURL string, xmlOut []byte, res sifenResult, estado string) {
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
		QRURL:           qrURL,
	}
	if err := s.resolver.ORDS().RegisterDocument(ctx, rec); err != nil {
		// No abortamos: la emisión ya ocurrió. Se deja en el log del proceso.
		fmt.Printf("[warn] register document %s: %v\n", cdc, err)
	}
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

// triggerKudeGeneration arma los datos del KuDE en la goroutine de la request
// (rde/*etree.Document no son concurrency-safe: no cruzan a otra goroutine) y
// lanza el render+subida en background con contexto propio. Best-effort: un
// fallo de Gotenberg/ORDS nunca afecta la respuesta ya enviada al cliente.
func (s *Server) triggerKudeGeneration(cfg *tenant.Config, rde *sifen.RDE, qrURL, cdc string) {
	data, err := kude.BuildKudeData(rde, qrURL, cfg.EnvUpper(), kude.Branding{
		TemplateID:    cfg.KudeConfig.TemplateID,
		ColorPrimario: cfg.KudeConfig.ColorPrimario,
		LogoURL:       cfg.KudeConfig.LogoURL,
		NotasFooter:   cfg.KudeConfig.NotasFooter,
	})
	if err != nil {
		fmt.Printf("[warn] kude %s: armando datos: %v\n", cdc, err)
		return
	}
	clientID := cfg.ClientID

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.kudeTimeout)
		defer cancel()

		html, err := kude.RenderKuDEHTML(data)
		if err != nil {
			fmt.Printf("[warn] kude %s: renderizando HTML: %v\n", cdc, err)
			return
		}
		pdf, err := s.gotenberg.Render(ctx, html)
		if err != nil {
			fmt.Printf("[warn] kude %s: Gotenberg: %v\n", cdc, err)
			return
		}
		if _, err := s.resolver.ORDS().UploadKude(ctx, clientID, cdc, pdf); err != nil {
			fmt.Printf("[warn] kude %s: subiendo a OCI: %v\n", cdc, err)
		}
	}()
}
