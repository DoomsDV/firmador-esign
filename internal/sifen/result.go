package sifen

import "regexp"

// Result resume la respuesta del WS de SIFEN (siRecepDE / siRecepEvento).
type Result struct {
	CodRes  string
	EstRes  string
	ProtAut string
	MsgRes  string
}

// ConsultaResult representa la respuesta de siConsDE. Found solo es verdadero
// para 0422; Cancelado se deriva del evento rGeVeCan incluido por SIFEN.
type ConsultaResult struct {
	Result
	Found     bool
	Cancelado bool
}

// Los tags llegan con prefijos de namespace variables (ns2:dCodRes, etc.); se
// extraen por nombre local ignorando el prefijo.
var (
	reCodRes      = regexp.MustCompile(`(?s)<(?:[\w-]+:)?dCodRes>\s*([^<]*?)\s*</(?:[\w-]+:)?dCodRes>`)
	reEstRes      = regexp.MustCompile(`(?s)<(?:[\w-]+:)?dEstRes>\s*([^<]*?)\s*</(?:[\w-]+:)?dEstRes>`)
	reProtAut     = regexp.MustCompile(`(?s)<(?:[\w-]+:)?dProtAut>\s*([^<]*?)\s*</(?:[\w-]+:)?dProtAut>`)
	reMsgRes      = regexp.MustCompile(`(?s)<(?:[\w-]+:)?dMsgRes>\s*([^<]*?)\s*</(?:[\w-]+:)?dMsgRes>`)
	reCancelacion = regexp.MustCompile(`(?s)<(?:[\w-]+:)?rGeVeCan(?:\s[^>]*)?/?\s*>`)
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

// ParseConsultaResult extrae el resultado de siConsDE y detecta la existencia
// de un evento de cancelación dentro de xContenDE.
func ParseConsultaResult(body []byte) ConsultaResult {
	result := ParseResult(body)
	return ConsultaResult{
		Result:    result,
		Found:     result.CodRes == "0422",
		Cancelado: result.CodRes == "0422" && reCancelacion.Match(body),
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

// EstadoDEResult diferencia una respuesta fiscal explícita de un problema de
// transporte o una respuesta SOAP incompleta. Solo el primer caso puede dejar
// al documento en RECHAZADO.
func EstadoDEResult(codRes string) (estado string, definitivo bool) {
	if codRes == "0260" {
		return "APROBADO", true
	}
	if len(codRes) == 4 {
		for _, r := range codRes {
			if r < '0' || r > '9' {
				return "FIRMADO", false
			}
		}
		return "RECHAZADO", true
	}
	return "FIRMADO", false
}

// EstadoEvento mapea el código a estado del evento. 0600 = "Evento registrado".
func EstadoEvento(codRes string) string {
	if codRes == "0600" {
		return "REGISTRADO"
	}
	return "RECHAZADO"
}
