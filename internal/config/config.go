package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"

	"github.com/DoomsDV/firmador-e/internal/sifen"
)

const (
	defaultCertP12Path = `C:\Users\HP\Documents\dnit-doc\7156160_identity_go.p12`
	defaultIdCSC       = "0001"
	defaultSifenEnv    = "test"

	defaultWsSync   = "https://sifen-test.set.gov.py/de/ws/sync/recibe.wsdl"
	defaultWsAsync  = "https://sifen-test.set.gov.py/de/ws/async/recibe-lote.wsdl"
	defaultWsEvento = "https://sifen-test.set.gov.py/de/ws/eventos/evento.wsdl"
	defaultQRBase   = "https://ekuatia.set.gov.py/consultas-test/qr?"

	NomEmiPrueba = "DE generado en ambiente de prueba - sin valor comercial ni fiscal"
)

type Config struct {
	DBUser          string
	DBPass          string
	DBHost          string
	DBPort          string
	DBService       string
	WalletPath      string
	CertP12Path     string
	CertP12Password string

	SifenEnv string // debe ser "test"
	IdCSC    string
	CSC      string

	RUC         string
	DV          int
	RazonSocial string
	Timbrado    string // 8 dígitos; en TEST = RUC sin DV paddeado
	FeIniT      string // YYYY-MM-DD inicio vigencia timbrado (TEST: fecha solicitud FE)
	Est         string
	PunExp      string

	WsSync   string
	WsAsync  string
	WsEvento string
	QRBase   string

	SendToSifen bool // si true, POST sync tras generar XML
}

func LoadConfig() *Config {
	_ = godotenv.Load()

	certPath := envOr("CERT_P12_PATH", defaultCertP12Path)
	env := strings.ToLower(strings.TrimSpace(envOr("SIFEN_ENV", defaultSifenEnv)))

	dv, _ := strconv.Atoi(envOr("SIFEN_DV", "8"))
	ruc := envOr("SIFEN_RUC", "6038964")
	// Guía DNIT (ambiente TEST): timbrado = RUC sin DV, pad a 8 dígitos con ceros.
	// Importante: string (no int) para conservar el cero a la izquierda (tdNumTim).
	// SIFEN_TIMBRADO_EXACT=1: enviar dNumTim tal cual (p.ej. 7 dígitos 6038964) sin pad.
	timbradoRaw := envOr("SIFEN_TIMBRADO", "")
	var timbrado string
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SIFEN_TIMBRADO_EXACT")), "1") && timbradoRaw != "" {
		timbrado = timbradoRaw
	} else if timbradoRaw != "" {
		timbrado = padTimbradoTest(timbradoRaw)
	} else {
		timbrado = padTimbradoTest(ruc)
	}

	send := true
	if v := strings.ToLower(os.Getenv("SIFEN_SEND")); v == "0" || v == "false" || v == "no" {
		send = false
	}

	cfg := &Config{
		DBUser:          os.Getenv("DB_USER"),
		DBPass:          os.Getenv("DB_PASS"),
		DBHost:          os.Getenv("DB_HOST"),
		DBPort:          os.Getenv("DB_PORT"),
		DBService:       os.Getenv("DB_SERVICE"),
		WalletPath:      os.Getenv("ORACLE_WALLET_PATH"),
		CertP12Path:     certPath,
		CertP12Password: os.Getenv("CERT_P12_PASSWORD"),
		SifenEnv:        env,
		IdCSC:           envOr("SIFEN_ID_CSC", defaultIdCSC),
		CSC:             os.Getenv("SIFEN_CSC"),
		RUC:             ruc,
		DV:              dv,
		RazonSocial:     envOr("SIFEN_RAZON_SOCIAL", "VILLASANTI VERGARA DANIEL RAMON"),
		Timbrado:        timbrado,
		FeIniT:          envOr("SIFEN_FE_INI_T", ""),
		Est:             envOr("SIFEN_EST", "001"),
		PunExp:          envOr("SIFEN_PUN_EXP", "001"),
		WsSync:          envOr("SIFEN_WS_SYNC", defaultWsSync),
		WsAsync:         envOr("SIFEN_WS_ASYNC", defaultWsAsync),
		WsEvento:        envOr("SIFEN_WS_EVENTO", defaultWsEvento),
		QRBase:          envOr("SIFEN_QR_BASE", defaultQRBase),
		SendToSifen:     send,
	}

	if cfg.CertP12Path == "" || cfg.CertP12Password == "" {
		log.Fatal("❌ Error: Faltan CERT_P12_PATH o CERT_P12_PASSWORD")
	}
	if cfg.CSC == "" {
		log.Fatal("❌ Error: Falta SIFEN_CSC")
	}

	if err := cfg.ValidateTestOnly(); err != nil {
		log.Fatalf("❌ BLOQUEO ANTI-PRODUCCIÓN: %v", err)
	}

	return cfg
}

// ValidateTestOnly garantiza que env y todas las URLs sean exclusivamente de prueba.
func (c *Config) ValidateTestOnly() error {
	if c.SifenEnv != "test" {
		return fmt.Errorf("SIFEN_ENV=%q no permitido; solo 'test' (nunca producción)", c.SifenEnv)
	}
	for _, u := range []string{c.WsSync, c.WsAsync, c.WsEvento, c.QRBase} {
		if err := sifen.AssertSafeTestURL(u); err != nil {
			return err
		}
	}
	return nil
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// padTimbradoTest: RUC sin DV → 8 dígitos (ceros a la izquierda). Guía DNIT TEST.
func padTimbradoTest(ruc string) string {
	ruc = strings.TrimSpace(ruc)
	if n, err := strconv.Atoi(ruc); err == nil {
		return fmt.Sprintf("%08d", n)
	}
	for len(ruc) < 8 {
		ruc = "0" + ruc
	}
	return ruc
}
