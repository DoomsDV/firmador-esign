// Command evento firma y envía eventos (cancelación / inutilización) al WS
// siRecepEvento de sifen-test.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/DoomsDV/firmador-e/internal/config"
	"github.com/DoomsDV/firmador-e/internal/sifen"
)

func main() {
	var (
		tipo     = flag.String("tipo", "cancelacion", "tipo de evento: cancelacion | inutilizacion")
		cdc      = flag.String("cdc", "", "CDC del DTE a cancelar (cancelacion)")
		motivo   = flag.String("motivo", "", "motivo del evento (5-500 caracteres)")
		idEvento = flag.Int("id", 1, "identificador del evento (Id de rEve)")
		numIn    = flag.Int("num-in", 0, "número inicial del rango (inutilizacion)")
		numFin   = flag.Int("num-fin", 0, "número final del rango (inutilizacion)")
		tiDE     = flag.Int("tide", 1, "tipo de DE del rango (inutilizacion)")
	)
	flag.Parse()

	cfg := config.LoadConfig()

	asuncion, err := time.LoadLocation("America/Asuncion")
	if err != nil {
		asuncion = time.FixedZone("PYT", -3*60*60)
	}
	fecha := time.Now().In(asuncion).Add(-1 * time.Minute)

	var ev *sifen.REve
	switch strings.ToLower(*tipo) {
	case "cancelacion":
		ev, err = sifen.NewEventoCancelacion(*idEvento, *cdc, *motivo, fecha)
	case "inutilizacion":
		ev, err = sifen.NewEventoInutilizacion(*idEvento, cfg.Timbrado, cfg.Est, cfg.PunExp, *numIn, *numFin, *tiDE, *motivo, fecha)
	default:
		log.Fatalf("❌ tipo de evento inválido: %q", *tipo)
	}
	if err != nil {
		log.Fatalf("❌ armar evento: %v", err)
	}

	fmt.Printf("Ambiente: TEST (sifen-test) — producción BLOQUEADA\n")
	fmt.Printf("Evento: %s (id=%d)\n", *tipo, *idEvento)

	cert, err := sifen.LoadCertificateFromFile(cfg.CertP12Path, cfg.CertP12Password)
	if err != nil {
		log.Fatalf("❌ cargando certificado: %v", err)
	}

	firmado, err := sifen.FirmarEvento(ev, cert)
	if err != nil {
		log.Fatalf("❌ firmar evento: %v", err)
	}
	fileName := fmt.Sprintf("evento_%s_%d.xml", *tipo, *idEvento)
	if err := os.WriteFile(fileName, firmado, 0644); err != nil {
		log.Fatalf("❌ escribiendo evento: %v", err)
	}
	fmt.Printf("✅ Evento firmado: %s\n", fileName)

	if !cfg.SendToSifen {
		fmt.Println("SIFEN_SEND=false — no se envía a sifen-test.")
		return
	}

	client, err := sifen.NewTestClient(cfg.CertP12Path, cfg.CertP12Password, cfg.WsSync)
	if err != nil {
		log.Fatalf("❌ cliente SOAP (test): %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	code, body, err := client.RecibirEventoSync(ctx, int64(*idEvento), cfg.WsEvento, firmado)
	if err != nil {
		log.Fatalf("❌ envío evento sifen-test: %v", err)
	}
	respFile := fmt.Sprintf("respuesta_evento_%s_%d.xml", *tipo, *idEvento)
	_ = os.WriteFile(respFile, body, 0644)
	fmt.Printf("HTTP %d — respuesta guardada en %s\n", code, respFile)
	fmt.Println(string(body))
}
