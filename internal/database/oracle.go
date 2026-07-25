package database

import (
	"database/sql"
	"fmt"
	"log"
	"net/url"

	// Importamos la config para saber las credenciales
	"github.com/DoomsDV/firmador-e/internal/config"
	_ "github.com/sijms/go-ora/v2"
)

var DB *sql.DB // Variable global del paquete

func InitOracle(cfg *config.Config) {
	fmt.Println("🔌 Conectando a Oracle Autonomous...")

	dsn := fmt.Sprintf("oracle://%s:%s@%s:%s/%s?wallet=%s&SSL=enable",
		cfg.DBUser, cfg.DBPass, cfg.DBHost, cfg.DBPort, cfg.DBService, url.QueryEscape(cfg.WalletPath))

	var err error
	DB, err = sql.Open("oracle", dsn)
	if err != nil {
		log.Fatalf("❌ Error abriendo driver: %v", err)
	}

	if err := DB.Ping(); err != nil {
		log.Fatalf("❌ Error de Ping a Oracle: %v", err)
	}

	fmt.Println("✅ ¡Conexión a Oracle Establecida!")
}
