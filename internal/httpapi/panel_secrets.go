package httpapi

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/DoomsDV/firmador-e/internal/sifen"
	"github.com/DoomsDV/firmador-e/internal/tenant"
)

type panelCertificateRequest struct {
	P12Base64 string `json:"p12_base64"`
	Password  string `json:"password"`
	SubjectDN string `json:"subject_dn"`
	NotAfter  string `json:"not_after"`
}

type panelEnvironmentRequest struct {
	Environment         string `json:"environment"`
	NumTimbrado         string `json:"num_timbrado"`
	FechaInicioVigencia string `json:"fecha_inicio_vigencia"`
	IdCSC               string `json:"id_csc"`
	CSC                 string `json:"csc"`
}

// handlePanelCertificate recibe .p12+password en claro, cifra con master key y
// persiste vía ORDS interno (certificate/store).
func (s *Server) handlePanelCertificate(w http.ResponseWriter, r *http.Request) {
	panel := panelFromContext(r.Context())
	var req panelCertificateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "body invalido: "+err.Error())
		return
	}
	if strings.TrimSpace(req.P12Base64) == "" || req.Password == "" {
		writeErr(w, http.StatusUnprocessableEntity, "VALIDATION", "p12_base64 y password son obligatorios")
		return
	}

	rawB64 := strings.ReplaceAll(strings.TrimSpace(req.P12Base64), "\n", "")
	p12, err := base64.StdEncoding.DecodeString(rawB64)
	if err != nil {
		p12, err = base64.RawStdEncoding.DecodeString(rawB64)
		if err != nil {
			writeErr(w, http.StatusUnprocessableEntity, "INVALID_P12", "p12_base64 invalido")
			return
		}
	}

	digital, err := sifen.OpenPKCS12(p12, req.Password)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "INVALID_P12", err.Error())
		return
	}

	subject := strings.TrimSpace(req.SubjectDN)
	if subject == "" {
		subject = digital.Cert.Certificate.Subject.String()
	}
	notAfter := strings.TrimSpace(req.NotAfter)
	if notAfter == "" {
		notAfter = digital.Cert.Certificate.NotAfter.UTC().Format("2006-01-02T15:04:05-07:00")
	}

	key := s.resolver.MasterKey()
	p12Nonce, p12CT, err := encHex(key, digital.StoredBytes)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ENCRYPT_ERROR", err.Error())
		return
	}
	pwdNonce, pwdCT, err := encHex(key, []byte(req.Password))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ENCRYPT_ERROR", err.Error())
		return
	}

	body := map[string]any{
		"client_id":      panel.ClientID,
		"role":           panel.Role,
		"subject_dn":     subject,
		"not_after":      notAfter,
		"p12_ciphertext": p12CT,
		"p12_nonce":      p12Nonce,
		"pwd_ciphertext": pwdCT,
		"pwd_nonce":      pwdNonce,
		"key_version":    1,
	}
	if err := s.resolver.ORDS().StoreCertificate(r.Context(), body); err != nil {
		writeErr(w, http.StatusBadGateway, "ORDS_ERROR", err.Error())
		return
	}
	writeOK(w, http.StatusOK, map[string]any{
		"status":     "ACTIVE",
		"subject_dn": subject,
		"not_after":  notAfter,
	})
}

// handlePanelEnvironments recibe timbrado+CSC en claro, cifra el CSC y persiste.
func (s *Server) handlePanelEnvironments(w http.ResponseWriter, r *http.Request) {
	panel := panelFromContext(r.Context())
	var req panelEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "body invalido: "+err.Error())
		return
	}
	env := strings.ToUpper(strings.TrimSpace(req.Environment))
	if env != "TEST" && env != "PROD" {
		writeErr(w, http.StatusUnprocessableEntity, "VALIDATION", "environment debe ser TEST o PROD")
		return
	}
	if req.NumTimbrado == "" || req.FechaInicioVigencia == "" || req.IdCSC == "" || req.CSC == "" {
		writeErr(w, http.StatusUnprocessableEntity, "VALIDATION", "num_timbrado, fecha_inicio_vigencia, id_csc y csc son obligatorios")
		return
	}
	if _, err := time.Parse("2006-01-02", req.FechaInicioVigencia); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "VALIDATION", "fecha_inicio_vigencia debe ser YYYY-MM-DD")
		return
	}

	nonce, ct, err := encHex(s.resolver.MasterKey(), []byte(req.CSC))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ENCRYPT_ERROR", err.Error())
		return
	}

	body := map[string]any{
		"client_id":             panel.ClientID,
		"role":                  panel.Role,
		"environment":           env,
		"num_timbrado":          strings.TrimSpace(req.NumTimbrado),
		"fecha_inicio_vigencia": req.FechaInicioVigencia,
		"id_csc":                strings.TrimSpace(req.IdCSC),
		"csc_ciphertext":        ct,
		"csc_nonce":             nonce,
		"key_version":           1,
	}
	if err := s.resolver.ORDS().StoreEnvironment(r.Context(), body); err != nil {
		writeErr(w, http.StatusBadGateway, "ORDS_ERROR", err.Error())
		return
	}
	writeOK(w, http.StatusOK, map[string]any{
		"environment":  env,
		"num_timbrado": req.NumTimbrado,
		"id_csc":       req.IdCSC,
	})
}

func encHex(key, plaintext []byte) (nonceHex, ctHex string, err error) {
	nonce, ct, err := tenant.Encrypt(key, plaintext)
	if err != nil {
		return "", "", err
	}
	return hex.EncodeToString(nonce), hex.EncodeToString(ct), nil
}
