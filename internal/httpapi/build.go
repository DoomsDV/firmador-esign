package httpapi

import (
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/DoomsDV/firmador-e/internal/sifen"
	"github.com/DoomsDV/firmador-e/internal/tenant"
)

// tipoDEFromString mapea el campo "tipo" del payload al iTiDE de SIFEN. La API Go
// expone FE/NCE/NDE (los tipos del Form.364 de producción). Autofactura y remisión
// quedan fuera de este endpoint por requerir grupos específicos (gCamAE/gTransp).
func tipoDEFromString(s string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "fe", "factura":
		return 1, nil
	case "nce", "notacredito":
		return 5, nil
	case "nde", "notadebito":
		return 6, nil
	case "auto", "autofactura", "nr", "remision":
		return 0, fmt.Errorf("tipo %q no soportado por la API v1 (usá fe|nce|nde)", s)
	default:
		return 0, fmt.Errorf("tipo de documento inválido: %q (fe|nce|nde)", s)
	}
}

// operacion agrupa el tipo de DE + establecimiento/punto resueltos del tenant.
// Se resuelve antes del correlativo (next-number) y se reutiliza al armar el input.
type operacion struct {
	TipoDE int
	Est    *tenant.Establecimiento
	Punto  string
}

// resolveOperacion valida tipo, establecimiento y punto contra la config del tenant.
func resolveOperacion(cfg *tenant.Config, req *createDocumentRequest) (*operacion, error) {
	tipoDE, err := tipoDEFromString(req.Tipo)
	if err != nil {
		return nil, err
	}
	est, err := cfg.SelectEstablecimiento(req.DatosOperacion.Establecimiento)
	if err != nil {
		return nil, err
	}
	punto, err := est.SelectPunto(req.DatosOperacion.PuntoExpedicion)
	if err != nil {
		return nil, err
	}
	return &operacion{TipoDE: tipoDE, Est: est, Punto: punto}, nil
}

// buildResult agrupa el input construido más metadatos usados luego (geo/receptor).
type buildResult struct {
	Input           sifen.DocumentInput
	TipoDE          int
	Establecimiento string
	Punto           string
	ReceptorNombre  string
	ReceptorDoc     string
	TotalRaw        decimal.Decimal
}

// buildDocumentInput arma el sifen.DocumentInput a partir del tenant resuelto y el
// payload. No toca el motor de firma: solo alimenta BuildDE con datos del tenant.
func buildDocumentInput(cfg *tenant.Config, req *createDocumentRequest, op *operacion, numeroDoc int, fecha time.Time) (*buildResult, error) {
	tipoDE := op.TipoDE
	est := op.Est
	punto := op.Punto

	if cfg.Timbrado.NumTimbrado == "" {
		return nil, fmt.Errorf("el tenant no tiene timbrado configurado para el ambiente %s", cfg.Environment)
	}

	emisor := cfg.BuildEmisor(est)

	rec, recNombre, recDoc, err := buildReceptor(&req.Receptor)
	if err != nil {
		return nil, err
	}

	moneda := strings.ToUpper(strings.TrimSpace(req.Moneda))
	if moneda == "" {
		moneda = "PYG"
	}

	items, totalRaw, err := buildItems(req.Items)
	if err != nil {
		return nil, err
	}

	tipTra := req.TipoTransaccion
	desTipTra := strings.TrimSpace(req.DesTipoTransaccion)
	if tipTra == 0 {
		tipTra = 1
		desTipTra = "Venta de mercadería"
	} else if desTipTra == "" {
		switch tipTra {
		case 2:
			desTipTra = "Prestación de servicios"
		default:
			desTipTra = "Venta de mercadería"
		}
	}

	indPres := req.IndPres
	desIndPres := strings.TrimSpace(req.DesIndPres)
	if indPres == 0 {
		indPres = 1
		desIndPres = "Operación presencial"
	} else if desIndPres == "" {
		switch indPres {
		case 2:
			desIndPres = "Operación electrónica"
		case 3:
			desIndPres = "Operación telemarketing"
		case 4:
			desIndPres = "Venta a domicilio"
		default:
			desIndPres = "Operación presencial"
		}
	}

	in := sifen.DocumentInput{
		TipoDE:             tipoDE,
		Establecimiento:    est.Codigo,
		PuntoExp:           punto,
		NumeroDoc:          numeroDoc,
		NumTimbrado:        cfg.Timbrado.NumTimbrado,
		FeIniT:             cfg.Timbrado.FechaInicioVigencia,
		TipoEmision:        1,
		FechaFirma:         fecha,
		Emisor:             emisor,
		Receptor:           rec,
		TipoTransaccion:    tipTra,
		DesTipoTransaccion: desTipTra,
		Moneda:             moneda,
		DesMoneda:          monedaDesc(moneda),
		Items:              items,
		IndPres:            indPres,
		DesIndPres:         desIndPres,
	}

	if !isPYG(moneda) {
		if req.TipoCambio == nil || !req.TipoCambio.IsPositive() {
			return nil, fmt.Errorf("moneda %s requiere tipoCambio > 0", moneda)
		}
		in.CondicionTipoCambio = 1 // tipo de cambio global
		in.TipoCambio = req.TipoCambio
	}

	switch tipoDE {
	case 1: // FE: requiere condición de pago
		cond, err := buildCondicion(req, totalRaw, moneda)
		if err != nil {
			return nil, err
		}
		in.Condicion = cond
	case 5, 6: // NCE / NDE: sin condición, con doc asociado y motivo
		in.MotivoEmision = req.Motivo
		if strings.TrimSpace(req.CdcRef) == "" {
			return nil, fmt.Errorf("%s requiere cdcRef (CDC de la FE referenciada)", req.Tipo)
		}
		asoc, err := sifen.NewDEAsocElectronico(strings.TrimSpace(req.CdcRef))
		if err != nil {
			return nil, err
		}
		in.DocsAsociados = []sifen.GCamDEAsoc{asoc}
	}

	return &buildResult{
		Input:           in,
		TipoDE:          tipoDE,
		Establecimiento: est.Codigo,
		Punto:           punto,
		ReceptorNombre:  recNombre,
		ReceptorDoc:     recDoc,
		TotalRaw:        totalRaw,
	}, nil
}

func buildReceptor(req *receptorRequest) (rec sifen.GDatRec, nombre, doc string, err error) {
	geo := toReceptorGeo(req.Geo)
	switch strings.ToLower(strings.TrimSpace(req.Tipo)) {
	case "ci":
		rec, err = sifen.NewReceptorCI(req.Documento, req.Nombre, geo)
		return rec, req.Nombre, req.Documento, err
	case "ruc":
		tipCont := req.TipoContribuyente
		if tipCont == 0 {
			tipCont = 2
		}
		rec, err = sifen.NewReceptorRUC(req.Documento, req.DV, tipCont, req.Nombre, req.TipoOperacion, geo)
		return rec, req.Nombre, req.Documento, err
	case "innominado", "":
		rec, err = sifen.NewReceptorInnominado()
		return rec, "Sin Nombre", "", err
	case "extranjero":
		tipoID := req.TipoIdentificacion
		if tipoID == 0 {
			tipoID = sifen.TipIDPasaporte
		}
		rec, err = sifen.NewReceptorExtranjero(req.Pais, req.DesPais, tipoID, req.Documento, req.Nombre, geo)
		return rec, req.Nombre, req.Documento, err
	default:
		return sifen.GDatRec{}, "", "", fmt.Errorf("receptor.tipo inválido: %q (ci|ruc|innominado|extranjero)", req.Tipo)
	}
}

func toReceptorGeo(g *geoRequest) sifen.ReceptorGeo {
	if g == nil {
		return sifen.ReceptorGeo{}
	}
	return sifen.ReceptorGeo{
		Dir:     g.Dir,
		NumCas:  g.NumCas,
		CDep:    g.DepCod,
		DDesDep: g.DepDesc,
		CDis:    g.DisCod,
		DDesDis: g.DisDesc,
		CCiu:    g.CiuCod,
		DDesCiu: g.CiuDesc,
		Tel:     g.Tel,
		Email:   g.Email,
	}
}

func buildItems(reqs []itemRequest) ([]sifen.ItemInput, decimal.Decimal, error) {
	if len(reqs) == 0 {
		return nil, decimal.Zero, fmt.Errorf("el documento no tiene ítems")
	}
	items := make([]sifen.ItemInput, 0, len(reqs))
	total := decimal.Zero
	for i, it := range reqs {
		if it.AfectacionIVA == 0 {
			it.AfectacionIVA = sifen.AfecIVAGravado
		}
		items = append(items, sifen.ItemInput{
			Codigo:          it.Codigo,
			Descripcion:     it.Descripcion,
			Cantidad:        it.Cantidad,
			PrecioUnitario:  it.PrecioUnitario,
			UnidadMedida:    it.UnidadMedida,
			DesUnidadMedida: it.DesUnidadMedida,
			AfectacionIVA:   it.AfectacionIVA,
			TasaIVA:         it.TasaIVA,
			PropIVA:         it.PropIVA,
		})
		if !it.Cantidad.IsPositive() {
			return nil, decimal.Zero, fmt.Errorf("ítem #%d (%s) requiere cantidad > 0", i+1, it.Codigo)
		}
		total = total.Add(it.PrecioUnitario.Mul(it.Cantidad))
	}
	return items, total, nil
}

// buildCondicion arma gCamCond. Contado usa un pago inicial por el total;
// medioPago opcional (default efectivo). Códigos sin descripción de catálogo se rechazan.
func buildCondicion(req *createDocumentRequest, total decimal.Decimal, moneda string) (*sifen.GCamCond, error) {
	switch strings.ToLower(strings.TrimSpace(req.Condicion)) {
	case "contado":
		if !isPYG(moneda) {
			return nil, fmt.Errorf("condición contado por API solo soportada en PYG (usá credito para %s)", moneda)
		}
		monto := total.Round(0)
		pago := sifen.PagoEfectivoPYG(monto)
		if req.MedioPago == 3 {
			pago = sifen.PagoTarjetaCreditoPYG(monto)
			if d := strings.TrimSpace(req.DesMedioPago); d != "" {
				pago.DDesTiPag = d
			}
		} else if req.MedioPago == 4 {
			pago = sifen.PagoTarjetaCreditoPYG(monto)
			pago.ITiPago = 4
			pago.DDesTiPag = "Tarjeta de débito"
			if d := strings.TrimSpace(req.DesMedioPago); d != "" {
				pago.DDesTiPag = d
			}
		} else if req.MedioPago > 0 {
			catalog := sifen.CatalogoDesTiPago(req.MedioPago)
			if catalog == "" {
				return nil, fmt.Errorf("medioPago %d no está en el catálogo SIFEN", req.MedioPago)
			}
			desc := strings.TrimSpace(req.DesMedioPago)
			if desc == "" {
				desc = catalog
			}
			pago.ITiPago = req.MedioPago
			pago.DDesTiPag = desc
			pago.GPagTarCD = nil
		}
		return sifen.CondicionContado(pago)
	case "credito", "":
		plazo := strings.TrimSpace(req.Plazo)
		if plazo == "" {
			plazo = "30"
		}
		return sifen.CondicionCreditoPlazo(plazo, nil, nil)
	default:
		return nil, fmt.Errorf("condición inválida: %q (contado|credito)", req.Condicion)
	}
}

func isPYG(moneda string) bool {
	return moneda == "" || strings.EqualFold(moneda, "PYG")
}

func monedaDesc(m string) string {
	switch strings.ToUpper(strings.TrimSpace(m)) {
	case "", "PYG":
		return "Guarani"
	case "USD":
		return "US Dollar"
	case "BRL":
		return "Brazilian Real"
	case "EUR":
		return "Euro"
	case "ARS":
		return "Argentine Peso"
	default:
		return m
	}
}
