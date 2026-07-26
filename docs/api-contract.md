# Contrato de API — Plataforma de Facturación Electrónica (SIFEN) multi-tenant

Versión: `v1` · Estado: diseño para el frontend futuro + referencia del backend ya implementado.

> También disponible como especificación **OpenAPI 3.0** en [`docs/openapi.yaml`](./openapi.yaml), abrible en Swagger UI / Redoc para documentación interactiva y generación de clientes.

Este documento describe los **tres planos** de la plataforma:

| Plano | Base URL | Autenticación | Consumidor | Estado |
|---|---|---|---|---|
| **Panel** | `{ORDS}/api/v1` (ORDS) | `Authorization: Bearer <JWT>` | Frontend del panel (futuro) | Implementado en ORDS (`db/ords/01_esign_module.sql`) |
| **Emisión** | `{GO}/v1` (Go) | `Authorization: Bearer sk_test_… / sk_prod_…` | Integradores / comercios | Implementado (`internal/httpapi`) y verificado 0260 |
| **Interno** | `{ORDS}/internal/v1` (ORDS) | `X-Service-Token: <token>` | Solo el servicio Go | Implementado (`db/ords/02_esign_internal_module.sql`) |

Donde:
- `{ORDS}` = `https://g9549f707e8ebfa-aoxdev.adb.sa-saopaulo-1.oraclecloudapps.com/ords/esign`
- `{GO}` = host del servidor Go (`SERVER_ADDR`, p. ej. `http://localhost:8080`).

El **ambiente SIFEN** (test/prod) NO se pasa por parámetro en el plano de emisión: lo determina exclusivamente el **prefijo de la API key** (`sk_test_` → SIFEN-test, `sk_prod_` → SIFEN-prod). Una key de test jamás puede pegarle a prod y viceversa (guardrail `AssertSafeURL`).

---

## 1. Envelope estándar de respuesta

Todas las respuestas (los tres planos) usan el mismo sobre JSON.

### Éxito

```json
{
  "success": true,
  "data": { },
  "error": null,
  "meta": { "page": 1, "pageSize": 20, "total": 0 }
}
```

- `data`: objeto o arreglo con el resultado. Puede ser `null` en operaciones sin cuerpo.
- `meta`: presente solo en listados paginados. Se omite en respuestas simples.

### Error

```json
{
  "success": false,
  "data": null,
  "error": { "code": "STRING_EN_MAYUSCULAS", "message": "descripción legible" }
}
```

El campo `error.code` es un identificador estable (no traducible); `error.message` es texto para diagnóstico.

### Códigos de error transversales

| `code` | HTTP | Significado |
|---|---|---|
| `UNAUTHORIZED` | 401 | Falta token / token inválido / `X-Service-Token` inválido |
| `INVALID_KEY_PREFIX` | 401 | La API key no empieza con `sk_test_`/`sk_prod_` |
| `INVALID_KEY` | 401 | La API key no resuelve a ningún cliente |
| `KEY_REVOKED` | 403 | La API key existe pero no está activa |
| `CLIENT_INACTIVE` | 403 | El cliente no está `ACTIVE` |
| `FORBIDDEN` | 403 | El rol no tiene permiso para la operación |
| `NOT_FOUND` | 404 | Recurso inexistente |
| `INVALID_JSON` | 400 | Body no es JSON válido |
| `VALIDATION_ERROR` | 422 | Payload válido como JSON pero inconsistente |
| `INTERNAL_ERROR` | 500 | Fallo inesperado |

---

## 2. Estados del ciclo de vida del documento

Estados canónicos que expone la plataforma (columna `document.estado`):

```
BORRADOR ──► FIRMADO ──► ENVIADO ──► APROBADO
                                  └─► RECHAZADO
APROBADO ──(evento cancelación 0600)──► CANCELADO
```

| Estado | Cuándo | Origen |
|---|---|---|
| `BORRADOR` | Documento creado, aún sin firmar | Panel (futuro) |
| `FIRMADO` | XML firmado pero no enviado a SIFEN (o envío falló) | Go, ante error de red al WS |
| `ENVIADO` | Enviado a SIFEN, sin respuesta terminal | Transitorio |
| `APROBADO` | SIFEN respondió `dCodRes=0260` | Go |
| `RECHAZADO` | SIFEN respondió un código distinto de 0260 | Go |
| `CANCELADO` | Evento de cancelación aceptado (`dCodRes=0600`) | Go (evento) |

Notas:
- En la implementación actual de Go, una emisión que loguea 0260 se persiste directamente como `APROBADO`; si el WS no responde, se persiste como `FIRMADO`.
- Los **eventos** (cancelación/inutilización) tienen su propio estado: `REGISTRADO` (`dCodRes=0600`) o `RECHAZADO`. Se guardan en `document_event`; la cancelación aceptada además transiciona el documento a `CANCELADO`.

---

## 3. Plano PANEL — ORDS `/api/v1` (Bearer JWT)

Handlers delgados en `db/ords/01_esign_module.sql` que delegan en los paquetes `PKG_ESIGN_*`. El JWT (emitido en login/select-client) transporta los claims `user_id`, `client_id` y `role`. La VPD (`ESIGN_CTX.CLIENT_ID`) aísla filas por cliente; el `role` restringe capacidades:

| Rol | Capacidades |
|---|---|
| `owner` | Todo (incluye certificado, environments, API keys, invitaciones) |
| `developer` | Ve/rota API keys y emite; NO gestiona certificado ni invitaciones |
| `analyst` | Solo LECTURA de documentos; 403 en certificado y API keys |

### 3.1 Autenticación y membresías

| Método | Ruta | Auth | Rol | Descripción |
|---|---|---|---|---|
| `POST` | `/api/v1/auth/register` | — | — | Crea `user` + `client` + membresía `owner` + `client_emisor` |
| `POST` | `/api/v1/auth/login` | — | — | Valida credenciales; si el user tiene varios clients devuelve la lista para elegir |
| `POST` | `/api/v1/auth/select-client` | — | — | Elige cliente y emite el JWT con ese `client_id` |
| `GET` | `/api/v1/auth/my-clients` | JWT | cualquiera | Lista los clients del usuario |
| `POST` | `/api/v1/auth/refresh` | — | — | Renueva el access token con el refresh token |
| `POST` | `/api/v1/auth/logout` | JWT | cualquiera | Revoca la sesión |
| `GET` | `/api/v1/auth/me` | JWT | cualquiera | Datos del usuario + membresía actual |
| `POST` | `/api/v1/invitations` | JWT | owner | Crea una invitación PENDING (email + role) con token/expiración |
| `POST` | `/api/v1/invitations/accept` | — | — | Acepta una invitación por token (pendiente: flujo por correo) |

**`POST /invitations`** — request `{ "email": "contador@empresa.com", "role": "analyst" }`; response:

```json
{ "success": true, "data": { "status": "INVITE_PENDING", "email": "contador@empresa.com", "role": "analyst", "token": "…", "expires_in_days": 7 } }
```

> Base para el futuro envío por correo: hoy solo persiste la invitación (tabla `client_invitation`) y devuelve el `token`. El envío del email y la aceptación (`/invitations/accept`, que creará el usuario + membresía) quedan pendientes.

**`POST /auth/register`** — request:

```json
{
  "email": "dueno@empresa.com",
  "password": "••••••••",
  "first_name": "Daniel",
  "last_name": "Villasanti",
  "business_name": "VILLASANTI VERGARA DANIEL RAMON",
  "ruc": "6038964",
  "dv": 8
}
```

**`POST /auth/login`** — request `{ "email": "...", "password": "..." }`; response (varios clients):

```json
{ "success": true, "data": { "user_id": 1, "clients": [ { "client_id": 4, "business_name": "…", "role": "owner" } ] } }
```

**`POST /auth/select-client`** — request `{ "user_id": 1, "client_id": 4 }`; response:

```json
{ "success": true, "data": { "access_token": "eyJ…", "refresh_token": "…", "expires_in": 3600, "role": "owner" } }
```

### 3.2 Cliente / emisor

| Método | Ruta | Rol | Descripción |
|---|---|---|---|
| `GET` | `/api/v1/client` | cualquiera | Identidad del negocio + emisor (tipo, régimen, actividades) |
| `PUT` | `/api/v1/client` | owner | Upsert del emisor a nivel contribuyente + actividades |

**`PUT /client`** — request:

```json
{
  "tipo_contribuyente": 1,
  "tipo_regimen": 8,
  "nombre_fantasia": "…",
  "actividades": [ { "cod": "74909", "desc": "OTRAS ACTIVIDADES PROFESIONALES, CIENTÍFICAS Y TÉCNICAS N.C.P." } ]
}
```

### 3.3 Establecimientos y puntos de expedición (jerarquía 1:N → 1:N)

Cada establecimiento (sucursal) tiene su propia dirección/geo (va en `gEmis` del DE). Los puntos de expedición (cajas) cuelgan del establecimiento.

| Método | Ruta | Rol | Descripción |
|---|---|---|---|
| `GET` | `/api/v1/establecimientos` | cualquiera | Lista de sucursales con geo |
| `POST` | `/api/v1/establecimientos` | owner | Alta/actualización de sucursal |
| `PUT` | `/api/v1/establecimientos/{codigo}` | owner | Actualiza una sucursal |
| `DELETE` | `/api/v1/establecimientos/{codigo}` | owner | Desactiva una sucursal |
| `POST` | `/api/v1/establecimientos/{codigo}/puntos` | owner | Alta/actualización de punto de expedición |
| `PUT` | `/api/v1/establecimientos/{codigo}/puntos/{codigo}` | owner | Actualiza un punto |
| `DELETE` | `/api/v1/establecimientos/{codigo}/puntos/{codigo}` | owner | Desactiva un punto |

**`POST /establecimientos`** — request:

```json
{
  "codigo": "001",
  "denominacion": "Casa Central",
  "direccion": "ROJAS CANADA, SAN JUAN-FRACCION. 9234",
  "num_casa": "0",
  "dep": { "cod": 12, "desc": "CENTRAL" },
  "dis": { "cod": 153, "desc": "CAPIATA" },
  "ciu": { "cod": 3568, "desc": "CAPIATA" },
  "telefono": "0986541799",
  "email": "correo@empresa.com"
}
```

> La geo debe coincidir con la registrada en el RUC/SET; si no, SIFEN devuelve error 1255.

### 3.4 Environments (timbrado / CSC)

| Método | Ruta | Rol | Descripción |
|---|---|---|---|
| `GET` | `/api/v1/environments/{test\|prod}` | owner | Timbrado, dFeIniT, IdCSC (nunca el CSC en claro) |
| `PUT` | `/api/v1/environments` | owner | Upsert del ambiente (timbrado + CSC cifrado) |

**`PUT /environments`** — request (el CSC llega **ya cifrado** en hex por el cliente/Go; ORDS nunca ve el claro):

```json
{
  "environment": "TEST",
  "num_timbrado": "06038964",
  "fecha_inicio_vigencia": "2026-07-09",
  "id_csc": "0001",
  "csc_ciphertext": "<hex>",
  "csc_nonce": "<hex>",
  "key_version": 1
}
```

### 3.5 API keys

| Método | Ruta | Rol | Descripción |
|---|---|---|---|
| `GET` | `/api/v1/api-keys` | owner/developer | Lista de keys (prefijo, ambiente, estado, label; nunca la key completa) |
| `POST` | `/api/v1/api-keys/{test\|prod}/rotate` | owner/developer | Genera una key nueva y la muestra **una sola vez** |

Las keys se guardan en la tabla hija `client_api_key` (rotables, con historial). Al rotar, las keys `ACTIVE` previas del mismo ambiente pasan a `REVOKED` y entra la nueva como `ACTIVE`.

**`GET /api-keys`** — response:

```json
{ "success": true, "data": { "keys": [ { "environment": "TEST", "prefix": "sk_test_ac41", "status": "ACTIVE", "label": "seed", "created_at": "2026-07-25T21:53:27-03:00" } ] } }
```

**`POST /api-keys/test/rotate`** — response (única vez que se ve la key completa):

```json
{ "success": true, "data": { "api_key": "sk_test_ABCD…", "prefix": "sk_test_ABCD", "environment": "TEST" } }
```

### 3.6 Certificado

El BLOB del `.p12` **nunca** se expone al panel; solo metadata. El upload llega ya cifrado (AES-256-GCM) por el cliente. Los certificados se guardan en la tabla hija `client_certificate` (historial); a lo sumo uno queda `ACTIVE` (el previo pasa a `INACTIVE` al subir uno nuevo).

| Método | Ruta | Rol | Descripción |
|---|---|---|---|
| `GET` | `/api/v1/certificate` | owner | Metadata del certificado `ACTIVE`: subject DN, vigencia, estado |
| `POST` | `/api/v1/certificate` | owner | Sube P12 cifrado + password cifrado + metadata (nuevo `ACTIVE`) |

`analyst` recibe `403 FORBIDDEN` en ambos.

**`POST /certificate`** — request:

```json
{
  "cert_subject_dn": "CN=…",
  "cert_not_after": "2027-01-01T00:00:00Z",
  "p12_ciphertext": "<hex>", "p12_nonce": "<hex>",
  "pwd_ciphertext": "<hex>", "pwd_nonce": "<hex>",
  "key_version": 1
}
```

### 3.7 Documentos (lectura)

| Método | Ruta | Rol | Descripción |
|---|---|---|---|
| `GET` | `/api/v1/documents` | cualquiera | Listado paginado con filtros |
| `GET` | `/api/v1/documents/{cdc}` | cualquiera | Detalle + estado SIFEN |
| `GET` | `/api/v1/documents/{cdc}/xml` | cualquiera | XML firmado (`application/xml`) |
| `GET` | `/api/v1/documents/{cdc}/kude` | cualquiera | KuDE (PDF), si disponible |
| `POST` | `/api/v1/documents/{cdc}/actions` | owner/developer | `{ "action": "cancel" \| "resend" }` (dispara Go) |

Query params de `GET /documents`: `environment`, `estado`, `tipo`, `desde`, `hasta`, `q`, `page`, `pageSize`. Response con `meta` de paginación:

```json
{
  "success": true,
  "data": [ { "cdc": "01…", "tipo_de": 1, "estado": "APROBADO", "cod_res": "0260", "prot_aut": "49766029", "receptor_nombre": "…", "total_operacion": 150000, "fecha_emision": "2026-07-25T…" } ],
  "meta": { "page": 1, "pageSize": 25, "total": 1 }
}
```

---

## 4. Plano EMISIÓN — Go `/v1` (Bearer sk_)

Servidor Go (`internal/httpapi`). Autenticación: `Authorization: Bearer sk_test_…` o `sk_prod_…`. El middleware deriva el ambiente del prefijo, resuelve el tenant contra ORDS interno, exige `client.status = ACTIVE` y coherencia key↔ambiente.

### 4.1 Endpoints

| Método | Ruta | Auth | Descripción |
|---|---|---|---|
| `GET` | `/v1/health` | — | Salud del servicio |
| `POST` | `/v1/documents` | sk_ | Crea, firma y envía un DE (FE/NCE/NDE) |
| `POST` | `/v1/documents/{cdc}/cancel` | sk_ | Evento de cancelación sobre un CDC |
| `POST` | `/v1/events/inutilizacion` | sk_ | Inutiliza un rango de numeración |

### 4.2 `POST /v1/documents`

Request:

```json
{
  "tipo": "fe",
  "condicion": "contado",
  "plazo": "30",
  "datos_operacion": { "establecimiento": "001", "punto_expedicion": "001" },
  "receptor": {
    "tipo": "ci",
    "documento": "1234567",
    "nombre": "Cliente Demo",
    "dv": 0,
    "tipoContribuyente": 0,
    "tipoOperacion": 0,
    "pais": "", "desPais": "",
    "tipoIdentificacion": 0,
    "geo": null
  },
  "moneda": "PYG",
  "tipoCambio": null,
  "items": [
    { "codigo": "SERV-001", "descripcion": "Servicio profesional", "cantidad": 1, "precioUnitario": 150000, "afectacionIVA": 1, "tasaIVA": 10, "unidadMedida": 77, "desUnidadMedida": "UNI", "propIVA": 100 }
  ],
  "cdcRef": "",
  "motivo": 0
}
```

Notas:
- `tipo`: `fe` (factura), `nce` (nota de crédito), `nde` (nota de débito). `nce`/`nde` requieren `cdcRef` y `motivo`. (`auto`/`nr` responden 422 en la API actual.)
- `receptor.tipo`: `ci` | `ruc` | `innominado` | `extranjero`. Para `ruc` completar `dv`, `tipoContribuyente`, `tipoOperacion`; para `extranjero` completar `pais`, `desPais`, `tipoIdentificacion`.
- `datos_operacion` es opcional: si se omite, Go usa el establecimiento/punto por defecto (primero activo) del cliente.
- Montos con precisión decimal (aceptan número o string JSON). No usar float64.
- `afectacionIVA`: 1 gravado, 2 exonerado, 3 exento, 4 gravado parcial.

Flujo interno (Go): resuelve `sk_` → `client_id`+ambiente → valida est/punto contra las tablas del cliente → toma la geo del establecimiento para `gEmis` → pide `numDoc` correlativo (`/internal/v1/next-number`) → genera CDC → firma con el cert descifrado → envía a SIFEN (test/prod) → persiste en ORDS (`/internal/v1/documents`).

Response `201 Created` (aprobado) / `200 OK` (rechazado por SIFEN):

```json
{
  "success": true,
  "data": {
    "cdc": "01060389648001001000000512026072511456298312",
    "estado": "APROBADO",
    "codRes": "0260",
    "protAut": "49766029",
    "mensaje": "Autorización del DE satisfactoria",
    "qr": "https://ekuatia.set.gov.py/consultas-test/qr?…",
    "numeroDocumento": "0000051",
    "ambiente": "test"
  }
}
```

Errores específicos: `NO_CERTIFICATE` (422), `INVALID_OPERATION` (422, est/punto inválido), `SEQUENCE_ERROR` (502), `BUILD_ERROR`/`INVALID_DOCUMENT` (422), `SIGN_ERROR` (500), `SEND_ERROR` (502, firmado pero no enviado → queda `FIRMADO`).

### 4.3 `POST /v1/documents/{cdc}/cancel`

Request:

```json
{ "motivo": "Error en los datos del documento electrónico" }
```

- `motivo`: texto libre 5–500 caracteres (`mOtEve`).
- El `{cdc}` es el CDC (44 dígitos) del documento aprobado a cancelar.

Response `200 OK`:

```json
{
  "success": true,
  "data": { "estado": "REGISTRADO", "codRes": "0600", "protAut": "1151875", "mensaje": "Evento registrado correctamente", "ambiente": "test" }
}
```

El evento se registra en `document_event` (`tipo_evento = CANCELACION`); el documento pasa a `CANCELADO` si `codRes=0600`.

### 4.4 `POST /v1/events/inutilizacion`

Request:

```json
{
  "establecimiento": "001",
  "punto_expedicion": "001",
  "numeroInicial": 1000,
  "numeroFinal": 1005,
  "tipoDE": 1,
  "motivo": "Numeración anulada por error de emisión"
}
```

- `numeroInicial`/`numeroFinal`: rango a inutilizar (≤ 1000 números por evento).
- `tipoDE`: tipo de documento del rango (1 FE, etc.).
- `motivo`: `mOtEve` 5–500 caracteres.

Response `200 OK` (mismo `eventResponse`):

```json
{
  "success": true,
  "data": { "estado": "REGISTRADO", "codRes": "0600", "protAut": "…", "mensaje": "Evento registrado correctamente", "ambiente": "test" }
}
```

Se registra en `document_event` (`tipo_evento = INUTILIZACION`, sin `cdc`).

### 4.5 `GET /v1/health`

Response `200 OK`:

```json
{ "success": true, "data": { "status": "ok", "time": "2026-07-25T21:00:00Z" } }
```

---

## 5. Plano INTERNO — ORDS `/internal/v1` (X-Service-Token)

Consumido **solo** por el servicio Go. Cada handler valida `X-Service-Token` contra `app_parameter.SERVICE_TOKEN`. La API key del comercio (en el body) resuelve `client_id`+ambiente. No debe exponerse fuera de la red del backend.

| Método | Ruta | In | Out |
|---|---|---|---|
| `POST` | `/internal/v1/context` | `{ "api_key": "sk_test_…" }` | client_id, environment, emisor, establecimientos/puntos, timbrado/CSC id, `cert_available` |
| `POST` | `/internal/v1/next-number` | `{ client_id, environment, establecimiento, punto_expedicion, tipo_de }` | `{ "number": N }` correlativo con bloqueo |
| `POST` | `/internal/v1/certificate` | `{ client_id }` | blobs cifrados (hex) + `key_version` |
| `POST` | `/internal/v1/csc` | `{ client_id, environment }` | CSC cifrado (hex) + `id_csc` + `key_version` |
| `POST` | `/internal/v1/documents` | `DocumentRecord` (client_id, cdc, estado, cod_res, prot_aut, xml_firmado, qr_url, …) | `{ success }` |
| `POST` | `/internal/v1/events` | `EventRecord` (client_id, cdc, tipo_evento, estado, cod_res, prot_aut, motivo) | `{ success }` |
| `POST` | `/internal/v1/logs` | `{ client_id, environment, endpoint, http_status, latency_ms }` | `{ success }` |

> El servidor Go registra automáticamente cada request de `/v1/*` en `api_log` vía este endpoint (middleware `withLogging`, asíncrono y best-effort: nunca bloquea ni falla el request). El `client_id`/`environment` se incluyen cuando el request está autenticado.

**`POST /internal/v1/context`** — response `data`:

```json
{
  "client_id": 4,
  "business_name": "DE generado en ambiente de prueba - sin valor comercial ni fiscal",
  "ruc": "6038964", "dv": 8, "status": "ACTIVE",
  "environment": "TEST", "cert_available": true,
  "emisor": {
    "tipo_contribuyente": 1, "tipo_regimen": 8, "nombre_fantasia": "…",
    "actividades": [ { "cod": "74909", "desc": "OTRAS ACTIVIDADES PROFESIONALES, CIENTÍFICAS Y TÉCNICAS N.C.P." } ]
  },
  "establecimientos": [
    { "codigo": "001", "denominacion": "…", "direccion": "…", "num_casa": "0",
      "dep": { "cod": 12, "desc": "CENTRAL" }, "dis": { "cod": 153, "desc": "CAPIATA" }, "ciu": { "cod": 3568, "desc": "CAPIATA" },
      "telefono": "…", "email": "…", "puntos": ["001"] }
  ],
  "sifen_env": { "num_timbrado": "06038964", "fecha_inicio_vigencia": "2026-07-09", "id_csc": "0001", "key_version": 1 }
}
```

**`POST /internal/v1/certificate`** — response `data`:

```json
{ "p12_ciphertext": "<hex>", "p12_nonce": "<hex>", "pwd_ciphertext": "<hex>", "pwd_nonce": "<hex>", "key_version": 1 }
```

**`POST /internal/v1/csc`** — response `data`:

```json
{ "id_csc": "0001", "csc_ciphertext": "<hex>", "csc_nonce": "<hex>", "key_version": 1 }
```

Notas de implementación (ADB / ORDS):
- El body se lee **una sola vez** en `l_body CLOB := :body_text;` (`:body` es BLOB y no liga con `json_value`; referenciar `:body_text` más de una vez en el mismo handler falla con ORDS-25001).
- Los ambientes se guardan en MAYÚSCULAS (`TEST`/`PROD`, restricción `CK_DOC_SEQ_ENV`). Go envía `EnvUpper()` a este plano; el motor SIFEN usa minúscula para URLs/guardrails.
- El cifrado/descifrado AES-256-GCM lo hace Go con `ESIGN_MASTER_KEY`; Oracle solo almacena ciphertext+nonce.

---

## 6. Convenciones generales

- **Content-Type**: `application/json; charset=utf-8` en request y response (salvo `…/xml`).
- **Idempotencia**: la emisión NO es idempotente; cada `POST /v1/documents` consume un correlativo. Reintentar solo tras `SEND_ERROR` con criterio (el CDC ya firmado queda `FIRMADO`).
- **Paginación**: `page` (1-based) + `pageSize`; el total va en `meta.total`.
- **Fechas**: ISO-8601. Las fechas de firma se calculan con hora de Asunción con margen atrás (evita error 1004).
- **Montos**: enteros o strings decimales; PYG sin decimales. Nunca float64 en el backend.
