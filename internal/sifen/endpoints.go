package sifen

// Endpoints agrupa las URLs de los WS y del QR de un ambiente SIFEN.
type Endpoints struct {
	WsSync   string // recibe.wsdl (siRecepDE síncrono)
	WsAsync  string // recibe-lote.wsdl (asíncrono por lote)
	WsEvento string // evento.wsdl (siRecepEvento)
	QRBase   string // base del dCarQR (consultas / consultas-test)
}

var endpointsByEnv = map[Environment]Endpoints{
	EnvTest: {
		WsSync:   "https://sifen-test.set.gov.py/de/ws/sync/recibe.wsdl",
		WsAsync:  "https://sifen-test.set.gov.py/de/ws/async/recibe-lote.wsdl",
		WsEvento: "https://sifen-test.set.gov.py/de/ws/eventos/evento.wsdl",
		QRBase:   "https://ekuatia.set.gov.py/consultas-test/qr?",
	},
	EnvProd: {
		WsSync:   "https://sifen.set.gov.py/de/ws/sync/recibe.wsdl",
		WsAsync:  "https://sifen.set.gov.py/de/ws/async/recibe-lote.wsdl",
		WsEvento: "https://sifen.set.gov.py/de/ws/eventos/evento.wsdl",
		QRBase:   "https://ekuatia.set.gov.py/consultas/qr?",
	},
}

// EndpointsFor devuelve las URLs oficiales del ambiente indicado. Ambiente
// desconocido cae a TEST (fail-safe: nunca producción por accidente).
func EndpointsFor(env Environment) Endpoints {
	if e, ok := endpointsByEnv[env]; ok {
		return e
	}
	return endpointsByEnv[EnvTest]
}
