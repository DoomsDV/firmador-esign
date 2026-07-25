// Command api genera y (opcionalmente) envía documentos electrónicos a sifen-test.
// Es un CLI fino: arma un sifen.DocumentInput según flags, delega la construcción
// del rDE en sifen.BuildDE y la firma/QR en sifen.FirmarYSerializar.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/DoomsDV/firmador-e/internal/config"
	"github.com/DoomsDV/firmador-e/internal/kude"
	"github.com/DoomsDV/firmador-e/internal/sifen"
)

func main() {
	var (
		tipoDoc  = flag.String("tipo", "fe", "tipo de documento: fe | auto | nce | nde | nr")
		modoPago = flag.String("pago", "credito", "condición de pago: contado | credito")
		tipoRec  = flag.String("receptor", "ci", "receptor: ci | ruc | innominado | extranjero")
		moneda   = flag.String("moneda", "PYG", "moneda de la operación: PYG | USD | ...")
		tiCamStr = flag.String("ticam", "", "tipo de cambio a PYG (requerido si moneda ≠ PYG)")
		mix      = flag.Bool("mix", false, "usar ítems con IVA mixto (10%%, 5%%, exento)")
		cdcRef   = flag.String("cdc-ref", "", "CDC de la FE referenciada (requerido para nce/nde)")
		motivo   = flag.Int("motivo", 1, "iMotEmi para nce/nde (1..8)")
		genKude  = flag.Bool("kude", false, "generar también el KuDE (PDF) del documento")
	)
	flag.Parse()

	cfg := config.LoadConfig()

	tipoDE, err := tipoDEFromFlag(*tipoDoc)
	if err != nil {
		log.Fatalf("❌ %v", err)
	}

	asuncion, errTZ := time.LoadLocation("America/Asuncion")
	if errTZ != nil {
		asuncion = time.FixedZone("PYT", -3*60*60)
	}
	// Margen atrás evita 1004 (firma adelantada respecto de la hora de SIFEN).
	fechaFirma := time.Now().In(asuncion).Add(-5 * time.Minute)
	numDoc := 200 + int(fechaFirma.Unix()%700) // correlativo de prueba distinto por envío

	feIniT := cfg.FeIniT
	if feIniT == "" {
		feIniT = fechaFirma.Format("2006-01-02")
	}

	emisor := buildEmisor(cfg)

	rec, err := buildReceptor(*tipoRec)
	if err != nil {
		log.Fatalf("❌ receptor: %v", err)
	}

	cond, err := buildCondicion(*modoPago)
	if err != nil {
		log.Fatalf("❌ condición de pago: %v", err)
	}

	items := buildItems(*mix)

	var tiCam *decimal.Decimal
	if !strings.EqualFold(*moneda, "PYG") {
		if *tiCamStr == "" {
			log.Fatalf("❌ moneda %s requiere -ticam", *moneda)
		}
		v, err := decimal.NewFromString(*tiCamStr)
		if err != nil {
			log.Fatalf("❌ -ticam inválido: %v", err)
		}
		tiCam = &v
	}

	in := sifen.DocumentInput{
		TipoDE:              tipoDE,
		Establecimiento:     cfg.Est,
		PuntoExp:            cfg.PunExp,
		NumeroDoc:           numDoc,
		NumTimbrado:         cfg.Timbrado,
		FeIniT:              feIniT,
		TipoEmision:         1,
		FechaFirma:          fechaFirma,
		Emisor:              emisor,
		Receptor:            rec,
		TipoTransaccion:     1,
		DesTipoTransaccion:  "Venta de mercadería",
		Moneda:              strings.ToUpper(*moneda),
		DesMoneda:           monedaDesc(*moneda),
		CondicionTipoCambio: condTiCam(*moneda),
		TipoCambio:          tiCam,
		Items:               items,
		Condicion:           cond,
		IndPres:             1,
		DesIndPres:          "Operación presencial",
	}

	if tipoDE == 5 || tipoDE == 6 {
		// NCE/NDE no llevan condición de pago; referencian la FE original.
		in.Condicion = nil
		in.MotivoEmision = *motivo
		if *cdcRef == "" {
			log.Fatalf("❌ %s requiere -cdc-ref (CDC de la FE original)", *tipoDoc)
		}
		asoc, err := sifen.NewDEAsocElectronico(*cdcRef)
		if err != nil {
			log.Fatalf("❌ documento asociado: %v", err)
		}
		in.DocsAsociados = []sifen.GCamDEAsoc{asoc}
	}

	if tipoDE == 4 {
		// Autofactura: el receptor debe ser el propio emisor (contribuyente, B2C,
		// dRucRec = dRucEm) y la condición de operación debe ser al contado (E601=1).
		selfRec, err := sifen.NewReceptorRUC(cfg.RUC, cfg.DV, emisor.TipoContribuyente,
			emisor.Nombre, 2, sifen.ReceptorGeo{
				Dir: emisor.Direccion, NumCas: emisor.NumeroCasa,
				CDep: emisor.Departamento, DDesDep: emisor.DesDepartamento,
				CDis: emisor.Distrito, DDesDis: emisor.DesDistrito,
				CCiu: emisor.Ciudad, DDesCiu: emisor.DesCiudad,
				Tel: emisor.Telefono, Email: emisor.Email,
			})
		if err != nil {
			log.Fatalf("❌ receptor autofactura: %v", err)
		}
		in.Receptor = selfRec
		cont, err := sifen.CondicionContado(sifen.PagoEfectivoPYG(decimal.NewFromInt(150000)))
		if err != nil {
			log.Fatalf("❌ condición autofactura: %v", err)
		}
		in.Condicion = cont
		camAE, err := sifen.NewCamAE(1, sifen.TipIDCedulaPY, "2545999",
			"VENDEDOR NO CONTRIBUYENTE DE PRUEBA", ubicacionPrueba(), ubicacionPrueba())
		if err != nil {
			log.Fatalf("❌ gCamAE: %v", err)
		}
		in.CamAE = camAE
		// La autofactura requiere documento asociado: constancia de no ser contribuyente.
		asoc, err := sifen.NewDEAsocConstancia(1, 0, "")
		if err != nil {
			log.Fatalf("❌ documento asociado (constancia): %v", err)
		}
		in.DocsAsociados = []sifen.GCamDEAsoc{asoc}
	}

	if tipoDE == 7 {
		// Nota de remisión: sin condición de pago, sin totales ni IVA por ítem;
		// requiere gCamNRE (motivo del traslado) y gTransp (datos del transporte).
		in.Condicion = nil
		in.Items = buildItemsRemision()
		camNRE, err := sifen.NewCamNRE(1, 1, 10) // traslado por ventas / emisor / 10 km
		if err != nil {
			log.Fatalf("❌ gCamNRE: %v", err)
		}
		// Motivo "Traslado por ventas" (E501=1) sin documento asociado exige dFecEm (E506).
		camNRE.DFecEm = fechaFirma.Format("2006-01-02")
		in.CamNRE = camNRE
		in.Transporte = buildTransporte(cfg, fechaFirma)
	}

	rde, err := sifen.BuildDE(in)
	if err != nil {
		log.Fatalf("❌ construir DE: %v", err)
	}

	cdc := rde.DE.Id
	feEmi := rde.DE.GDatGralOpe.DFeEmiDE
	fmt.Printf("Ambiente: TEST (sifen-test) — producción BLOQUEADA\n")
	fmt.Printf("Tipo=%s CDC=%s\n", *tipoDoc, cdc)
	fmt.Printf("Escenario: pago=%s receptor=%s moneda=%s timbrado=%s\n",
		*modoPago, *tipoRec, in.Moneda, cfg.Timbrado)
	fmt.Printf("Fecha firma (America/Asuncion -5m): %s\n", feEmi)

	fmt.Println("--- Firmando Documento (Exc-C14N etree) ---")
	cert, err := sifen.LoadCertificateFromFile(cfg.CertP12Path, cfg.CertP12Password)
	if err != nil {
		log.Fatalf("❌ cargando certificado: %v", err)
	}

	// La nota de remisión no lleva gTotSub: el QR usa dTotGralOpe=0 y dTotIVA=0.
	var totGralOpe, totIVA decimal.Decimal
	if rde.DE.GTotSub != nil {
		totGralOpe = rde.DE.GTotSub.DTotGralOpe
		totIVA = rde.DE.GTotSub.DTotIVA
	}
	var qrURL string
	digestValue, xmlOut, err := sifen.FirmarYSerializar(rde, cert, func(dv string) (string, error) {
		u, err := sifen.BuildCarQR(sifen.QRParams{
			CDC:         cdc,
			FeEmiDE:     feEmi,
			RecField:    sifen.ReceptorFieldForQR(rde.DE.GDatGralOpe.GDatRec),
			RecID:       sifen.ReceptorIDForQR(rde.DE.GDatGralOpe.GDatRec),
			TotGralOpe:  sifen.DecimalPlain(totGralOpe),
			TotIVA:      sifen.DecimalPlain(totIVA),
			Items:       len(rde.DE.GDtipDE.GCamItem),
			DigestValue: dv,
			IdCSC:       cfg.IdCSC,
			CSC:         cfg.CSC,
			QRBase:      cfg.QRBase,
		})
		qrURL = u
		return u, err
	})
	if err != nil {
		log.Fatalf("❌ al firmar: %v", err)
	}
	fmt.Printf("✅ Documento firmado (digest=%s)\n", digestValue)

	if err := smokeChecks(xmlOut, in.Moneda, tipoDE); err != nil {
		log.Fatalf("❌ %v", err)
	}
	fmt.Println("✅ Smoke UTF-8/schema OK")

	fileName := fmt.Sprintf("%s_%s.xml", *tipoDoc, cdc)
	if err := os.WriteFile(fileName, xmlOut, 0644); err != nil {
		log.Fatalf("❌ escribiendo XML: %v", err)
	}
	fmt.Printf("\n✅ XML Generado: %s\n", fileName)

	if *genKude {
		pdf, err := kude.RenderKuDE(rde, qrURL)
		if err != nil {
			log.Fatalf("❌ generando KuDE: %v", err)
		}
		pdfName := fmt.Sprintf("%s_%s.pdf", *tipoDoc, cdc)
		if err := os.WriteFile(pdfName, pdf, 0644); err != nil {
			log.Fatalf("❌ escribiendo KuDE: %v", err)
		}
		fmt.Printf("✅ KuDE Generado: %s\n", pdfName)
	}

	if !cfg.SendToSifen {
		fmt.Println("SIFEN_SEND=false — no se envía a sifen-test.")
		return
	}

	fmt.Println("--- Enviando a SIFEN TEST (sync recibe.wsdl) ---")
	client, err := sifen.NewTestClient(cfg.CertP12Path, cfg.CertP12Password, cfg.WsSync)
	if err != nil {
		log.Fatalf("❌ cliente SOAP (test): %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	code, body, err := client.RecibirDESync(ctx, int64(numDoc), xmlOut)
	if err != nil {
		log.Fatalf("❌ envío sifen-test: %v", err)
	}
	respFile := fmt.Sprintf("respuesta_%s.xml", cdc)
	_ = os.WriteFile(respFile, body, 0644)
	fmt.Printf("HTTP %d — respuesta guardada en %s\n", code, respFile)
	fmt.Println(string(body))
}

func tipoDEFromFlag(s string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "fe", "factura":
		return 1, nil
	case "auto", "autofactura", "afe":
		return 4, nil
	case "nce", "notacredito":
		return 5, nil
	case "nde", "notadebito":
		return 6, nil
	case "nr", "remision", "nre":
		return 7, nil
	default:
		return 0, fmt.Errorf("tipo de documento inválido: %q (fe|auto|nce|nde|nr)", s)
	}
}

// buildEmisor arma el emisor de homologación (valores exactos que lograron 0260).
func buildEmisor(cfg *config.Config) sifen.Emisor {
	actividad := envOr("SIFEN_ACT_ECO", "74909")
	descAct := envOr("SIFEN_ACT_DESC", "OTRAS ACTIVIDADES PROFESIONALES, CIENTÍFICAS Y TÉCNICAS N.C.P.")
	return sifen.Emisor{
		RUC:               cfg.RUC,
		DV:                cfg.DV,
		TipoContribuyente: 1,
		TipoRegimen:       8,
		// Homologación: nombre fijo exigido por DNIT (no la razón social real).
		Nombre:       config.NomEmiPrueba,
		Direccion:    "ROJAS CAÑADA, SAN JUAN-FRACCION. 9234",
		NumeroCasa:   "0",
		Departamento: 12, DesDepartamento: "CENTRAL",
		Distrito: 153, DesDistrito: "CAPIATA",
		Ciudad: 3568, DesCiudad: "CAPIATA",
		Telefono: "0986541799",
		Email:    "DANIELVILLASANTI2132@GMAIL.COM",
		Actividades: []sifen.GActEco{
			{CActEco: actividad, DDesActEco: descAct},
		},
	}
}

func buildReceptor(tipo string) (sifen.GDatRec, error) {
	geo := sifen.ReceptorGeo{
		Dir: "DIRECCION PARTICULAR", NumCas: "123",
		CDep: 12, DDesDep: "CENTRAL",
		CDis: 153, DDesDis: "CAPIATA",
		CCiu: 3568, DDesCiu: "CAPIATA",
		Tel: "0981111111", Email: "cliente@email.com",
	}
	switch strings.ToLower(tipo) {
	case "ci":
		return sifen.NewReceptorCI("1234567", "CLIENTE DE PRUEBA", geo)
	case "ruc":
		return sifen.NewReceptorRUC("80012345", 6, 2, "EMPRESA CLIENTE SA", 1, geo)
	case "innominado":
		return sifen.NewReceptorInnominado()
	case "extranjero":
		return sifen.NewReceptorExtranjero("BRA", "Brasil", sifen.TipIDPasaporte, "AB1234567", "CLIENTE DEL EXTERIOR", geo)
	default:
		return sifen.GDatRec{}, fmt.Errorf("receptor inválido: %q", tipo)
	}
}

func buildCondicion(modo string) (*sifen.GCamCond, error) {
	switch strings.ToLower(modo) {
	case "contado":
		return sifen.CondicionContado(sifen.PagoEfectivoPYG(decimal.NewFromInt(150000)))
	case "credito":
		return sifen.CondicionCreditoPlazo("28", nil, nil)
	default:
		return nil, fmt.Errorf("modo de pago inválido: %q", modo)
	}
}

// buildItems arma los ítems de prueba. Con mix=true emite tasas mixtas para
// validar la Fase 1 (10%%, 5%% y exento).
func buildItems(mix bool) []sifen.ItemInput {
	itemDesc := "DOCUMENTO ELECTRÓNICO SIN VALOR COMERCIAL NI FISCAL - GENERADO EN AMBIENTE DE PRUEBA"
	if !mix {
		return []sifen.ItemInput{{
			Codigo: "SERV-001", Descripcion: itemDesc,
			Cantidad: decimal.NewFromInt(1), PrecioUnitario: decimal.NewFromInt(150000),
			AfectacionIVA: sifen.AfecIVAGravado, TasaIVA: 10,
		}}
	}
	return []sifen.ItemInput{
		{
			Codigo: "SERV-010", Descripcion: itemDesc + " (10%)",
			Cantidad: decimal.NewFromInt(1), PrecioUnitario: decimal.NewFromInt(110000),
			AfectacionIVA: sifen.AfecIVAGravado, TasaIVA: 10,
		},
		{
			Codigo: "SERV-005", Descripcion: itemDesc + " (5%)",
			Cantidad: decimal.NewFromInt(1), PrecioUnitario: decimal.NewFromInt(105000),
			AfectacionIVA: sifen.AfecIVAGravado, TasaIVA: 5,
		},
		{
			Codigo: "SERV-EXE", Descripcion: itemDesc + " (exento)",
			Cantidad: decimal.NewFromInt(1), PrecioUnitario: decimal.NewFromInt(50000),
			AfectacionIVA: sifen.AfecIVAExento, TasaIVA: 0,
		},
	}
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

func condTiCam(m string) int {
	if strings.EqualFold(strings.TrimSpace(m), "PYG") || m == "" {
		return 0
	}
	return 1 // tipo de cambio global
}

// smokeChecks valida invariantes de encoding y schema antes de enviar.
func smokeChecks(xmlOut []byte, moneda string, tipoDE int) error {
	s := string(xmlOut)
	for _, want := range []string{"ROJAS CAÑADA"} {
		if !strings.Contains(s, want) {
			return fmt.Errorf("smoke UTF-8 falló: falta %q en el XML firmado", want)
		}
	}
	// La nota de remisión (C002=7) no informa gTotSub: no aplican las invariantes de totales.
	if tipoDE == 7 {
		if strings.Contains(s, "<gTotSub>") {
			return fmt.Errorf("smoke schema: la nota de remisión no debe informar gTotSub")
		}
		if !strings.Contains(s, "<gTransp>") {
			return fmt.Errorf("smoke schema: la nota de remisión requiere gTransp")
		}
		return nil
	}
	if strings.EqualFold(moneda, "PYG") && strings.Contains(s, "<dTotalGs>") {
		return fmt.Errorf("smoke schema: dTotalGs no debe existir cuando cMoneOpe=PYG")
	}
	if !strings.EqualFold(moneda, "PYG") && !strings.Contains(s, "<dTotalGs>") {
		return fmt.Errorf("smoke schema: dTotalGs es obligatorio cuando cMoneOpe≠PYG")
	}
	if strings.Contains(s, "<dMonEnt>") {
		return fmt.Errorf("smoke schema: dMonEnt no debe existir sin entrega inicial")
	}
	return nil
}

// ubicacionPrueba devuelve una ubicación de homologación (CENTRAL/CAPIATÁ) para
// el vendedor y el lugar de la transacción de la autofactura.
func ubicacionPrueba() sifen.UbicacionAE {
	return sifen.UbicacionAE{
		Direccion: "ROJAS CAÑADA, SAN JUAN-FRACCION. 9234", NumeroCasa: "0",
		Departamento: 12, DesDepartamento: "CENTRAL",
		Distrito: 153, DesDistrito: "CAPIATA",
		Ciudad: 3568, DesCiudad: "CAPIATA",
	}
}

// buildItemsRemision arma ítems de una nota de remisión (sin valor ni IVA).
func buildItemsRemision() []sifen.ItemInput {
	itemDesc := "MERCADERIA SIN VALOR COMERCIAL NI FISCAL - GENERADA EN AMBIENTE DE PRUEBA"
	return []sifen.ItemInput{{
		Codigo: "MERC-001", Descripcion: itemDesc,
		Cantidad: decimal.NewFromInt(2), UnidadMedida: 77, DesUnidadMedida: "UNI",
	}}
}

// buildTransporte arma un gTransp terrestre completo (salida, entrega, vehículo y
// transportista) para la nota de remisión.
func buildTransporte(cfg *config.Config, fecha time.Time) *sifen.GTransp {
	descTipo, _ := sifen.DescTipoTransporte(1)  // propio
	descMod, _ := sifen.DescModalidadTransporte(1) // terrestre
	return &sifen.GTransp{
		ITipTrans: 1, DDesTipTrans: descTipo,
		IModTrans: 1, DDesModTrans: descMod,
		IRespFlete: 1, // emisor de la factura
		DIniTras:   fecha.Format("2006-01-02"),
		DFinTras:   fecha.AddDate(0, 0, 1).Format("2006-01-02"),
		GCamSal: &sifen.GCamSal{
			DDirLocSal: "DEPOSITO CENTRAL DE PRUEBA", DNumCasSal: "0",
			CDepSal: 12, DDesDepSal: "CENTRAL",
			CDisSal: 153, DDesDisSal: "CAPIATA",
			CCiuSal: 3568, DDesCiuSal: "CAPIATA",
		},
		GCamEnt: []sifen.GCamEnt{{
			DDirLocEnt: "DIRECCION DE ENTREGA DE PRUEBA", DNumCasEnt: "0",
			CDepEnt: 12, DDesDepEnt: "CENTRAL",
			CDisEnt: 153, DDesDisEnt: "CAPIATA",
			CCiuEnt: 3568, DDesCiuEnt: "CAPIATA",
		}},
		GVehTras: []sifen.GVehTras{{
			DTiVehTras: "CAMION", DMarVeh: "SCANIA",
			DTipIdenVeh: 2, DNroMatVeh: "ABC123",
		}},
		GCamTrans: &sifen.GCamTrans{
			INatTrans: 1, DNomTrans: "TRANSPORTISTA DE PRUEBA SA",
			DRucTrans: cfg.RUC, DDVTrans: cfg.DV,
			DNumIDChof: "2545999", DNomChof: "CHOFER DE PRUEBA",
			DDomFisc: "ROJAS CAÑADA, SAN JUAN-FRACCION. 9234",
			DDirChof: "DIRECCION DEL CHOFER DE PRUEBA",
		},
	}
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
