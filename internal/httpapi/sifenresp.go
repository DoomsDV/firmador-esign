package httpapi

import "regexp"

// sifenResult resume la respuesta del WS de SIFEN (siRecepDE / siRecepEvento).
type sifenResult struct {
	CodRes  string
	EstRes  string
	ProtAut string
	MsgRes  string
}

// Los tags llegan con prefijos de namespace variables (ns2:dCodRes, etc.); se
// extraen por nombre local ignorando el prefijo.
var (
	reCodRes  = regexp.MustCompile(`(?s)<(?:[\w-]+:)?dCodRes>\s*([^<]*?)\s*</(?:[\w-]+:)?dCodRes>`)
	reEstRes  = regexp.MustCompile(`(?s)<(?:[\w-]+:)?dEstRes>\s*([^<]*?)\s*</(?:[\w-]+:)?dEstRes>`)
	reProtAut = regexp.MustCompile(`(?s)<(?:[\w-]+:)?dProtAut>\s*([^<]*?)\s*</(?:[\w-]+:)?dProtAut>`)
	reMsgRes  = regexp.MustCompile(`(?s)<(?:[\w-]+:)?dMsgRes>\s*([^<]*?)\s*</(?:[\w-]+:)?dMsgRes>`)
)

func parseSifenResult(body []byte) sifenResult {
	s := string(body)
	return sifenResult{
		CodRes:  firstGroup(reCodRes, s),
		EstRes:  firstGroup(reEstRes, s),
		ProtAut: firstGroup(reProtAut, s),
		MsgRes:  firstGroup(reMsgRes, s),
	}
}

func firstGroup(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// estadoDE mapea el código de respuesta al estado canónico del documento.
// 0260 = "Autorización del DE satisfactoria".
func estadoDE(codRes string) string {
	if codRes == "0260" {
		return "APROBADO"
	}
	return "RECHAZADO"
}

// estadoEvento mapea el código a estado del evento. 0600 = "Evento registrado".
func estadoEvento(codRes string) string {
	if codRes == "0600" {
		return "REGISTRADO"
	}
	return "RECHAZADO"
}
