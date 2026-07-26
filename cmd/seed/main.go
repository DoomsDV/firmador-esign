// Command seed siembra el tenant de prueba (Villasanti) en el esquema esign a
// partir de testdata/sifen_test_fixtures.json y del .env. Cifra el certificado
// .p12 y el CSC en Go (AES-256-GCM con ESIGN_MASTER_KEY) y los carga vía los
// paquetes PL/SQL (que gestionan la VPD). Deja el tenant listo para emitir por API.
//
// La API key sk_test_ se genera en Go (mismo hash SHA-256 que ORDS) y se imprime
// UNA sola vez. Uso: go run ./cmd/seed [-email ...] [-password ...]
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	goora "github.com/sijms/go-ora/v2"

	"github.com/DoomsDV/firmador-e/internal/config"
	"github.com/DoomsDV/firmador-e/internal/sifen"
	"github.com/DoomsDV/firmador-e/internal/tenant"
)

type codeDesc struct {
	Cod  int    `json:"cod"`
	Desc string `json:"desc"`
}

type fixtures struct {
	Emisor struct {
		NombreEmisorTest string   `json:"nombreEmisorTest"`
		Direccion        string   `json:"direccion"`
		NumCasa          string   `json:"numCasa"`
		Departamento     codeDesc `json:"departamento"`
		Distrito         codeDesc `json:"distrito"`
		Ciudad           codeDesc `json:"ciudad"`
		Telefono         string   `json:"telefono"`
		Email            string   `json:"email"`
		ActividadEco     struct {
			Cod  string `json:"cod"`
			Desc string `json:"desc"`
		} `json:"actividadEconomica"`
	} `json:"emisor"`
}

func main() {
	var (
		email        = flag.String("email", "seed+villasanti@esign.local", "email del owner del tenant semilla")
		password     = flag.String("password", "Seed_Villasanti_2026!", "password del owner")
		fixturesPath = flag.String("fixtures", "testdata/sifen_test_fixtures.json", "ruta al JSON de fixtures")
	)
	flag.Parse()

	cfg := config.LoadConfig()

	masterKey, err := tenant.LoadMasterKey()
	if err != nil {
		log.Fatalf("❌ %v", err)
	}

	fx := loadFixtures(*fixturesPath)

	// --- Cifrado en Go (AES-256-GCM) del certificado, su password y el CSC ---
	p12Bytes, err := os.ReadFile(cfg.CertP12Path)
	if err != nil {
		log.Fatalf("❌ leer p12 %q: %v", cfg.CertP12Path, err)
	}
	digital, err := sifen.LoadCertificateFromBytes(p12Bytes, cfg.CertP12Password)
	if err != nil {
		log.Fatalf("❌ el p12 no se pudo decodificar (revisar CERT_P12_PASSWORD): %v", err)
	}

	p12Nonce, p12CT := encHex(masterKey, p12Bytes)
	pwdNonce, pwdCT := encHex(masterKey, []byte(cfg.CertP12Password))
	cscNonce, cscCT := encHex(masterKey, []byte(cfg.CSC))

	subjectDN := digital.Certificate.Subject.String()
	notAfter := digital.Certificate.NotAfter.UTC().Format("2006-01-02T15:04:05-07:00")

	// --- API key sk_test_ (misma derivación de hash que ORDS: SHA-256 hex) ---
	apiKey := genAPIKey()
	prefix := apiKey[:12]
	sum := sha256.Sum256([]byte(apiKey))
	keyHash := hex.EncodeToString(sum[:])

	// --- Bodies JSON para los paquetes PL/SQL ---
	regBody := mustJSON(map[string]any{
		"email":              *email,
		"password":           *password,
		"business_name":      fx.Emisor.NombreEmisorTest, // dNomEmi fijo de homologación en TEST
		"ruc":                cfg.RUC,
		"dv":                 cfg.DV,
		"tipo_contribuyente": 1,
		"tipo_regimen":       8,
	})
	loginBody := mustJSON(map[string]any{"email": *email, "password": *password})
	emisorBody := mustJSON(map[string]any{
		"tipo_contribuyente": 1,
		"tipo_regimen":       8,
		"actividades": []map[string]any{
			{"cod": fx.Emisor.ActividadEco.Cod, "desc": fx.Emisor.ActividadEco.Desc},
		},
	})
	estabBody := mustJSON(map[string]any{
		"codigo":       cfg.Est,
		"denominacion": "Casa Matriz",
		"direccion":    fx.Emisor.Direccion,
		"num_casa":     fx.Emisor.NumCasa,
		"dep":          map[string]any{"cod": fx.Emisor.Departamento.Cod, "desc": fx.Emisor.Departamento.Desc},
		"dis":          map[string]any{"cod": fx.Emisor.Distrito.Cod, "desc": fx.Emisor.Distrito.Desc},
		"ciu":          map[string]any{"cod": fx.Emisor.Ciudad.Cod, "desc": fx.Emisor.Ciudad.Desc},
		"telefono":     fx.Emisor.Telefono,
		"email":        fx.Emisor.Email,
	})
	puntoBody := mustJSON(map[string]any{"codigo": cfg.PunExp, "descripcion": "Caja principal"})
	envBody := mustJSON(map[string]any{
		"environment":           "TEST",
		"num_timbrado":          cfg.Timbrado,
		"fecha_inicio_vigencia": cfg.FeIniT,
		"id_csc":                cfg.IdCSC,
		"csc_ciphertext":        cscCT,
		"csc_nonce":             cscNonce,
		"key_version":           1,
	})
	certBody := mustJSON(map[string]any{
		"subject_dn":     subjectDN,
		"not_after":      notAfter,
		"p12_ciphertext": p12CT,
		"p12_nonce":      p12Nonce,
		"pwd_ciphertext": pwdCT,
		"pwd_nonce":      pwdNonce,
		"key_version":    1,
	})

	// --- Conexión Oracle (go-ora, mismo DSN que internal/database) ---
	dsn := fmt.Sprintf("oracle://%s:%s@%s:%s/%s?wallet=%s&SSL=enable",
		cfg.DBUser, cfg.DBPass, cfg.DBHost, cfg.DBPort, cfg.DBService, url.QueryEscape(cfg.WalletPath))
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		log.Fatalf("❌ abrir driver Oracle: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Una única sesión dedicada: el seed y el ALTER SESSION deben ir juntos.
	conn, err := db.Conn(ctx)
	if err != nil {
		log.Fatalf("❌ conexión Oracle: %v", err)
	}
	defer conn.Close()
	if err := conn.PingContext(ctx); err != nil {
		log.Fatalf("❌ ping Oracle: %v", err)
	}
	fmt.Println("✅ Conectado a esign (Oracle ADB)")

	// La ADB trae DML paralelo activo: los MERGE con dependencias pueden dar
	// ORA-12860/12839. Se desactiva en esta sesión para el sembrado idempotente.
	if _, err := conn.ExecContext(ctx, "ALTER SESSION DISABLE PARALLEL DML"); err != nil {
		log.Fatalf("❌ disable parallel dml: %v", err)
	}

	if _, err := conn.ExecContext(ctx, seedBlock,
		sql.Named("reg_body", goora.Clob{String: regBody, Valid: true}),
		sql.Named("login_body", goora.Clob{String: loginBody, Valid: true}),
		sql.Named("emisor_body", goora.Clob{String: emisorBody, Valid: true}),
		sql.Named("estab_body", goora.Clob{String: estabBody, Valid: true}),
		sql.Named("estab_cod", cfg.Est),
		sql.Named("punto_body", goora.Clob{String: puntoBody, Valid: true}),
		sql.Named("env_body", goora.Clob{String: envBody, Valid: true}),
		sql.Named("cert_body", goora.Clob{String: certBody, Valid: true}),
		sql.Named("pfx", prefix),
		sql.Named("hash", keyHash),
	); err != nil {
		log.Fatalf("❌ sembrado: %v", err)
	}

	fmt.Println("✅ Tenant semilla creado/actualizado en esign")
	fmt.Println("   RUC:", cfg.RUC, "DV:", cfg.DV, "| business_name:", fx.Emisor.NombreEmisorTest)
	fmt.Println("   Establecimiento:", cfg.Est, "Punto:", cfg.PunExp, "| Timbrado:", cfg.Timbrado, "dFeIniT:", cfg.FeIniT)
	fmt.Println("   Certificado:", subjectDN, "| vence:", notAfter)
	fmt.Println()
	fmt.Println("🔑 API KEY (se muestra UNA sola vez — guardala):")
	fmt.Println("   " + apiKey)
	fmt.Println()
	fmt.Println("Probar:  curl -X POST http://localhost:8080/v1/documents \\")
	fmt.Println("           -H \"Authorization: Bearer " + apiKey + "\" \\")
	fmt.Println("           -H \"Content-Type: application/json\" -d @payload.json")
}

// seedBlock crea (o reutiliza) el tenant y carga toda la config en una sola
// transacción. La API key se fija con hash precomputado en Go (evita OUT params).
const seedBlock = `
DECLARE
  l_out CLOB;
  l_cid NUMBER;
BEGIN
  BEGIN
    pkg_esign_auth_api.pr_register(:reg_body, l_out);
    l_cid := json_value(l_out, '$.data.client_id');
  EXCEPTION WHEN OTHERS THEN
    IF INSTR(SQLERRM, 'ORA-20409') > 0 THEN
      pkg_esign_auth_api.pr_login(:login_body, l_out);
      l_cid := json_value(l_out, '$.data.clients[0].client_id');
    ELSE
      RAISE;
    END IF;
  END;

  pkg_esign_client_api.pr_upsert_emisor(l_cid, 'owner', :emisor_body, l_out);
  pkg_esign_client_api.pr_upsert_establecimiento(l_cid, 'owner', :estab_body, l_out);
  pkg_esign_client_api.pr_upsert_punto(l_cid, 'owner', :estab_cod, :punto_body, l_out);
  pkg_esign_client_api.pr_upsert_env(l_cid, 'owner', :env_body, l_out);
  pkg_esign_cert_api.pr_put_certificate(l_cid, 'owner', :cert_body, l_out);

  pkg_esign_session.set_client(l_cid);
  UPDATE client SET sk_test_prefix = :pfx, sk_test_hash = :hash, sk_test_status = 'ACTIVE'
   WHERE id_client = l_cid;

  COMMIT;
END;`

func encHex(key, plaintext []byte) (nonceHex, ctHex string) {
	nonce, ct, err := tenant.Encrypt(key, plaintext)
	if err != nil {
		log.Fatalf("❌ cifrar: %v", err)
	}
	return hex.EncodeToString(nonce), hex.EncodeToString(ct)
}

func genAPIKey() string {
	buf := make([]byte, 24) // 48 hex
	if _, err := rand.Read(buf); err != nil {
		log.Fatalf("❌ generar API key: %v", err)
	}
	return "sk_test_" + hex.EncodeToString(buf)
}

func loadFixtures(path string) fixtures {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("❌ leer fixtures %q: %v", path, err)
	}
	var fx fixtures
	if err := json.Unmarshal(data, &fx); err != nil {
		log.Fatalf("❌ parsear fixtures: %v", err)
	}
	if fx.Emisor.NombreEmisorTest == "" {
		log.Fatalf("❌ fixtures sin emisor.nombreEmisorTest")
	}
	return fx
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		log.Fatalf("❌ serializar JSON: %v", err)
	}
	return string(b)
}
