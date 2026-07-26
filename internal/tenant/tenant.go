package tenant

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/DoomsDV/firmador-e/internal/sifen"
)

// Config es el contexto resuelto de un tenant para un ambiente concreto. Reemplaza
// los valores que antes vivían hardcodeados en cmd/api/main.go e internal/config.
type Config struct {
	ClientID      int
	BusinessName  string // razón social (dNomEmi)
	RUC           string
	DV            int
	Status        string // ACTIVE | SUSPENDED | INACTIVE
	Environment   sifen.Environment
	CertAvailable bool

	Emisor           EmisorConfig
	Establecimientos []Establecimiento
	Timbrado         TimbradoConfig // del ambiente resuelto
}

// EmisorConfig son los datos del emisor a nivel contribuyente (no la sucursal).
type EmisorConfig struct {
	TipoContribuyente int
	TipoRegimen       int
	NombreFantasia    string
	Actividades       []Actividad
}

// Actividad económica (cActEco/dDesActEco).
type Actividad struct {
	Cod  string
	Desc string
}

// Geo es un par código/descripción de la referencia geográfica oficial.
type Geo struct {
	Cod  int
	Desc string
}

// Establecimiento (sucursal) con su propia dirección/geo: cada local reporta a la
// SET su ciudad/dirección (gEmis se arma desde aquí, no de una casa matriz).
type Establecimiento struct {
	Codigo       string
	Denominacion string
	Direccion    string
	NumCasa      string
	Dep          Geo
	Dis          Geo
	Ciu          Geo
	Telefono     string
	Email        string
	Puntos       []string
}

// TimbradoConfig es el timbrado + CSC (id/vigencia) del ambiente. El valor del CSC
// se obtiene descifrado aparte (GetCSC); aquí solo va el identificador.
type TimbradoConfig struct {
	NumTimbrado         string
	FechaInicioVigencia string // YYYY-MM-DD
	IdCSC               string
	KeyVersion          int
}

// EnvUpper devuelve el ambiente en la forma canónica de la BD/ORDS (TEST|PROD).
// El motor sifen usa la forma en minúscula (EnvTest/EnvProd) para URLs y guardrails;
// las tablas esign lo almacenan en mayúscula (check constraints, UPPER en procs).
func (c *Config) EnvUpper() string {
	return strings.ToUpper(string(c.Environment))
}

// BuildEmisor arma el sifen.Emisor combinando la identidad del contribuyente con
// la dirección/geo del establecimiento emisor seleccionado.
func (c *Config) BuildEmisor(est *Establecimiento) sifen.Emisor {
	acts := make([]sifen.GActEco, 0, len(c.Emisor.Actividades))
	for _, a := range c.Emisor.Actividades {
		acts = append(acts, sifen.GActEco{CActEco: a.Cod, DDesActEco: a.Desc})
	}
	return sifen.Emisor{
		RUC:               c.RUC,
		DV:                c.DV,
		TipoContribuyente: c.Emisor.TipoContribuyente,
		TipoRegimen:       c.Emisor.TipoRegimen,
		Nombre:            c.BusinessName,
		NombreFantasia:    c.Emisor.NombreFantasia,
		Direccion:         est.Direccion,
		NumeroCasa:        est.NumCasa,
		Departamento:      est.Dep.Cod,
		DesDepartamento:   est.Dep.Desc,
		Distrito:          est.Dis.Cod,
		DesDistrito:       est.Dis.Desc,
		Ciudad:            est.Ciu.Cod,
		DesCiudad:         est.Ciu.Desc,
		Telefono:          est.Telefono,
		Email:             est.Email,
		Actividades:       acts,
	}
}

// SelectEstablecimiento resuelve el establecimiento por código. Si codigo está
// vacío, usa el primero disponible (default del tenant).
func (c *Config) SelectEstablecimiento(codigo string) (*Establecimiento, error) {
	codigo = strings.TrimSpace(codigo)
	if len(c.Establecimientos) == 0 {
		return nil, fmt.Errorf("el tenant no tiene establecimientos configurados")
	}
	if codigo == "" {
		return &c.Establecimientos[0], nil
	}
	for i := range c.Establecimientos {
		if c.Establecimientos[i].Codigo == codigo {
			return &c.Establecimientos[i], nil
		}
	}
	return nil, fmt.Errorf("establecimiento %q no existe o está inactivo para este tenant", codigo)
}

// SelectPunto valida que el punto de expedición pertenezca al establecimiento. Si
// punto está vacío, usa el primero. Devuelve el código validado.
func (est *Establecimiento) SelectPunto(punto string) (string, error) {
	punto = strings.TrimSpace(punto)
	if len(est.Puntos) == 0 {
		return "", fmt.Errorf("el establecimiento %q no tiene puntos de expedición activos", est.Codigo)
	}
	if punto == "" {
		return est.Puntos[0], nil
	}
	for _, p := range est.Puntos {
		if p == punto {
			return punto, nil
		}
	}
	return "", fmt.Errorf("punto de expedición %q no existe o está inactivo en el establecimiento %q", punto, est.Codigo)
}

// Certificate es el .p12 descifrado en memoria, listo para firmar y para mTLS.
type Certificate struct {
	P12        []byte
	Password   string
	KeyVersion int
}

// Resolver traduce una API key en un Config del tenant, cacheando el resultado
// con TTL corto. Descifra certificado y CSC con la clave maestra AES-256-GCM.
type Resolver struct {
	ords      *ORDSClient
	masterKey []byte
	ttl       time.Duration

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	cfg     *Config
	expires time.Time
}

// NewResolver crea el resolver. ttl<=0 desactiva el cache.
func NewResolver(ords *ORDSClient, masterKey []byte, ttl time.Duration) *Resolver {
	return &Resolver{
		ords:      ords,
		masterKey: masterKey,
		ttl:       ttl,
		cache:     make(map[string]cacheEntry),
	}
}

// ORDS expone el cliente interno para operaciones que no dependen del cache
// (next-number, register document/event, logs).
func (r *Resolver) ORDS() *ORDSClient {
	return r.ords
}

// Resolve devuelve el Config del tenant a partir de la API key. El ambiente se
// deriva del prefijo de la key (validado en la capa HTTP) y debe coincidir con el
// que reporta ORDS. Resultados cacheados por api key con TTL corto.
func (r *Resolver) Resolve(ctx context.Context, apiKey string, expectedEnv sifen.Environment) (*Config, error) {
	if cfg := r.fromCache(apiKey); cfg != nil {
		if cfg.Environment != expectedEnv {
			return nil, fmt.Errorf("ambiente incoherente: la key es %s pero el contexto es %s", expectedEnv, cfg.Environment)
		}
		return cfg, nil
	}

	resp, err := r.ords.resolveContext(ctx, apiKey)
	if err != nil {
		return nil, err
	}
	cfg, err := toConfig(resp)
	if err != nil {
		return nil, err
	}
	if cfg.Environment != expectedEnv {
		return nil, fmt.Errorf("ambiente incoherente: la key es %s pero ORDS resolvió %s", expectedEnv, cfg.Environment)
	}
	r.store(apiKey, cfg)
	return cfg, nil
}

// GetCertificate obtiene y descifra el certificado .p12 del tenant.
func (r *Resolver) GetCertificate(ctx context.Context, cfg *Config) (*Certificate, error) {
	return r.GetCertificateByClientID(ctx, cfg.ClientID)
}

// GetCertificateByClientID obtiene y descifra el certificado por client_id
// (worker de reenvio: no tiene Config completo vía API key).
func (r *Resolver) GetCertificateByClientID(ctx context.Context, clientID int) (*Certificate, error) {
	resp, err := r.ords.getCertificate(ctx, clientID)
	if err != nil {
		return nil, err
	}
	p12, err := DecryptHex(r.masterKey, resp.P12Nonce, resp.P12Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("descifrar certificado: %w", err)
	}
	pwd, err := DecryptHex(r.masterKey, resp.PwdNonce, resp.PwdCiphertext)
	if err != nil {
		return nil, fmt.Errorf("descifrar contraseña del certificado: %w", err)
	}
	return &Certificate{P12: p12, Password: string(pwd), KeyVersion: resp.KeyVersion}, nil
}

// MasterKey expone la clave AES-256 para cifrar secretos del panel (mediación).
func (r *Resolver) MasterKey() []byte {
	return r.masterKey
}

// GetCSC obtiene y descifra el CSC del ambiente del tenant (necesario para el QR).
func (r *Resolver) GetCSC(ctx context.Context, cfg *Config) (string, error) {
	resp, err := r.ords.getCSC(ctx, cfg.ClientID, cfg.EnvUpper())
	if err != nil {
		return "", err
	}
	csc, err := DecryptHex(r.masterKey, resp.CSCNonce, resp.CSCCiphertext)
	if err != nil {
		return "", fmt.Errorf("descifrar CSC: %w", err)
	}
	return string(csc), nil
}

func (r *Resolver) fromCache(apiKey string) *Config {
	if r.ttl <= 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.cache[apiKey]
	if !ok || time.Now().After(e.expires) {
		if ok {
			delete(r.cache, apiKey)
		}
		return nil
	}
	return e.cfg
}

func (r *Resolver) store(apiKey string, cfg *Config) {
	if r.ttl <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[apiKey] = cacheEntry{cfg: cfg, expires: time.Now().Add(r.ttl)}
}

// toConfig mapea la respuesta de ORDS al Config tipado.
func toConfig(resp *contextResponse) (*Config, error) {
	env, err := sifen.ParseEnvironment(resp.Environment)
	if err != nil {
		return nil, fmt.Errorf("ambiente del contexto inválido: %w", err)
	}
	cfg := &Config{
		ClientID:      resp.ClientID,
		BusinessName:  resp.BusinessName,
		RUC:           resp.RUC,
		DV:            resp.DV,
		Status:        resp.Status,
		Environment:   env,
		CertAvailable: resp.CertAvailable,
		Emisor: EmisorConfig{
			TipoContribuyente: resp.Emisor.TipoContribuyente,
			TipoRegimen:       resp.Emisor.TipoRegimen,
			NombreFantasia:    resp.Emisor.NombreFantasia,
		},
	}
	for _, a := range resp.Emisor.Actividades {
		cfg.Emisor.Actividades = append(cfg.Emisor.Actividades, Actividad{Cod: a.Cod, Desc: a.Desc})
	}
	for _, e := range resp.Establecimientos {
		cfg.Establecimientos = append(cfg.Establecimientos, Establecimiento{
			Codigo:       e.Codigo,
			Denominacion: e.Denominacion,
			Direccion:    e.Direccion,
			NumCasa:      e.NumCasa,
			Dep:          Geo{Cod: e.Dep.Cod, Desc: e.Dep.Desc},
			Dis:          Geo{Cod: e.Dis.Cod, Desc: e.Dis.Desc},
			Ciu:          Geo{Cod: e.Ciu.Cod, Desc: e.Ciu.Desc},
			Telefono:     e.Telefono,
			Email:        e.Email,
			Puntos:       e.Puntos,
		})
	}
	if resp.SifenEnv != nil {
		cfg.Timbrado = TimbradoConfig{
			NumTimbrado:         resp.SifenEnv.NumTimbrado,
			FechaInicioVigencia: resp.SifenEnv.FechaInicioVigencia,
			IdCSC:               resp.SifenEnv.IdCSC,
			KeyVersion:          resp.SifenEnv.KeyVersion,
		}
	}
	return cfg, nil
}
