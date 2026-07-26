package sifen

import "regexp"

// Result resume la respuesta del WS de SIFEN (siRecepDE / siRecepEvento).
type Result struct {
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

// ParseResult extrae códigos de la respuesta XML del WS SIFEN.
func ParseResult(body []byte) Result {
	s := string(body)
	return Result{
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

// EstadoDE mapea el código de respuesta al estado canónico del documento.
// 0260 = "Autorización del DE satisfactoria".
func EstadoDE(codRes string) string {
	if codRes == "0260" {
		return "APROBADO"
	}
	return "RECHAZADO"
}

// EstadoEvento mapea el código a estado del evento. 0600 = "Evento registrado".
func EstadoEvento(codRes string) string {
	if codRes == "0600" {
		return "REGISTRADO"
	}
	return "RECHAZADO"
}
