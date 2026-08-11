# Guía de integración — API esign (SIFEN multi-tenant)

Documento práctico: **qué URL llamar**, **qué token usar**, **qué JSON enviar** y **qué respuesta esperar**.

> Documentación completa: [`api-contract.md`](./api-contract.md) · OpenAPI emisión: [`openapi-emision.yaml`](./openapi-emision.yaml) · Índice de specs: [`openapi/README.md`](./openapi/README.md)

---

## 1. Tres planos de la API

| Plano | Base URL | Autenticación | ¿Para quién? |
|---|---|---|---|
| **Emisión (homologación)** | `https://api-staging.etick.uno/v1` | `Authorization: Bearer sk_test_…` | Integradores en pruebas SIFEN |
| **Emisión (producción)** | `https://api.etick.uno/v1` | `Authorization: Bearer sk_prod_…` | Integradores en producción |
| **Panel** | `https://g9549f707e8ebfa-aoxdev.adb.sa-saopaulo-1.oraclecloudapps.com/ords/esign/api/v1` | `Authorization: Bearer <JWT>` | Frontend esign (configuración y consulta) |
| **Panel → Go** | `https://api-staging.etick.uno/v1/panel` o `https://api.etick.uno/v1/panel` | `Authorization: Bearer <JWT>` (solo `owner`) | Subida de certificado y CSC en claro (Go cifra) |
| **Interno** | `{ORDS}/internal/v1` | `X-Service-Token: <token>` | Solo el servicio Go (no exponer) |

Bases para integradores:

```text
GO_TEST = https://api-staging.etick.uno    # + sk_test_…
GO_PROD = https://api.etick.uno            # + sk_prod_…
```

**Regla clave:** el ambiente SIFEN (test/prod) **no** se manda como parámetro en emisión. Lo define el **prefijo de la API key**:

- `sk_test_…` → SIFEN homologación (`sifen-test`)
- `sk_prod_…` → SIFEN producción

---

## 2. Envelope JSON (todas las respuestas)

### Éxito

```json
{
  "success": true,
  "data": { },
  "error": null
}
```

Listados paginados incluyen además `"meta": { "page": 1, "pageSize": 25, "total": 100 }`.

### Error

```json
{
  "success": false,
  "data": null,
  "error": { "code": "VALIDATION_ERROR", "message": "descripción legible" }
}
```

Headers comunes:

```http
Content-Type: application/json; charset=utf-8
Accept: application/json
```

---

## 3. Emisión de documentos (integradores)

### Obtener una API key

1. Entrar al panel esign como `owner` o `developer`.
2. `POST {ORDS}/api/v1/api-keys/test/rotate` (o `…/prod/rotate`) con JWT.
3. Guardar la key completa — **solo se muestra una vez**:

```json
{
  "success": true,
  "data": {
    "api_key": "<copiar del panel; solo visible al rotar>",
    "prefix": "<prefijo visible; empieza con sk_test_>",
    "environment": "TEST"
  }
}
```

### Emitir una factura (`POST /v1/documents`)

**URL (homologación):** `https://api-staging.etick.uno/v1/documents`  
**URL (producción):** `https://api.etick.uno/v1/documents`  
**Auth:** `Authorization: Bearer sk_test_…` (homologación) o `sk_prod_…` (producción)

**Body mínimo (FE contado, PYG, receptor innominado):**

```json
{
  "tipo": "fe",
  "condicion": "contado",
  "datos_operacion": {
    "establecimiento": "001",
    "punto_expedicion": "001"
  },
  "receptor": { "tipo": "innominado" },
  "moneda": "PYG",
  "items": [
    {
      "codigo": "SERV-001",
      "descripcion": "Servicio profesional",
      "cantidad": 1,
      "precioUnitario": 150000,
      "afectacionIVA": 1,
      "tasaIVA": 10
    }
  ]
}
```

**Receptor con CI:**

```json
"receptor": {
  "tipo": "ci",
  "documento": "1234567",
  "nombre": "Cliente Demo"
}
```

**Receptor contribuyente (RUC):**

```json
"receptor": {
  "tipo": "ruc",
  "documento": "80012345",
  "dv": 6,
  "nombre": "Empresa Demo S.A.",
  "tipoContribuyente": 2,
  "tipoOperacion": 1
}
```

**Nota de crédito (NCE):** agregar `cdcRef` (CDC de la FE aprobada) y `motivo` (entero catálogo SET); `"tipo": "nce"`.

| Campo | Valores / notas |
|---|---|
| `tipo` | `fe`, `nce`, `nde` |
| `condicion` | `contado`, `credito` (+ `plazo` en días si crédito) |
| `moneda` | `PYG`, `USD`, … (descripción SET exacta en XML) |
| `tipoCambio` | Obligatorio si moneda ≠ PYG |
| `afectacionIVA` | `1` gravado, `2` exonerado, `3` exento, `4` parcial |
| `precioUnitario` / montos | Número o string decimal; **no** float impreciso en clientes |

**Respuesta aprobada (`201`):**

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

**Ejemplo curl:**

```bash
curl -sS -X POST "https://api-staging.etick.uno/v1/documents" \
  -H "Authorization: Bearer sk_test_<tu_key>" \
  -H "Content-Type: application/json" \
  -d '{
    "tipo": "fe",
    "condicion": "contado",
    "datos_operacion": { "establecimiento": "001", "punto_expedicion": "001" },
    "receptor": { "tipo": "innominado" },
    "moneda": "PYG",
    "items": [{
      "codigo": "A1",
      "descripcion": "Producto demo",
      "cantidad": 1,
      "precioUnitario": 150000,
      "afectacionIVA": 1,
      "tasaIVA": 10
    }]
  }'
```

### Cancelar un documento

**URL:** `https://api-staging.etick.uno/v1/documents/{cdc}/cancel` (homologación) · `https://api.etick.uno/v1/documents/{cdc}/cancel` (producción)  
**Body:** `{ "motivo": "Error en los datos del documento electrónico" }` (5–500 caracteres)

### Inutilizar numeración

**URL:** `https://api-staging.etick.uno/v1/events/inutilizacion` (homologación) · `https://api.etick.uno/v1/events/inutilizacion` (producción)  
**Body:**

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

### Health check

`GET https://api-staging.etick.uno/v1/health` o `GET https://api.etick.uno/v1/health` — sin auth.

---

## 4. Panel web (JWT)

Flujo típico del frontend:

1. `POST {ORDS}/api/v1/auth/login` → `{ email, password }`
2. Si hay varios negocios: `POST {ORDS}/api/v1/auth/select-client` → `{ user_id, client_id }`
3. Usar `access_token` en todas las llamadas al panel:

```http
Authorization: Bearer eyJhbGciOi…
```

Claims relevantes del JWT: `client_id`, `role` (`owner` | `developer` | `analyst`).

### Endpoints ORDS más usados

| Método | Ruta | Rol | Descripción |
|---|---|---|---|
| `GET` | `/api/v1/client` | cualquiera | Negocio + emisor + actividades |
| `PUT` | `/api/v1/client` | owner | Emisor (`nombre_fantasia`, actividades, …) |
| `GET` | `/api/v1/establecimientos` | cualquiera | Sucursales y puntos |
| `GET` | `/api/v1/environments/{test\|prod}` | owner | Timbrado guardado (sin CSC) |
| `GET` | `/api/v1/api-keys` | owner/dev | Lista de keys (solo prefijo) |
| `POST` | `/api/v1/api-keys/{test\|prod}/rotate` | owner/dev | Nueva key (se muestra 1 vez) |
| `GET` | `/api/v1/documents` | cualquiera | Listado (`?environment=&estado=&page=`) |
| `GET` | `/api/v1/documents/{cdc}/kude` | cualquiera | PDF KuDE |
| `GET` | `/api/v1/kude-config` | cualquiera | Branding KuDE |
| `PUT` | `/api/v1/kude-config` | owner | Plantilla, color, footer, mostrar fantasía |
| `POST` | `/api/v1/kude-config/logo` | owner | Subir logo (hex + mime) |

**`PUT /api/v1/client`:**

```json
{
  "tipo_contribuyente": 1,
  "tipo_regimen": 8,
  "nombre_fantasia": "HASEL",
  "actividades": [
    { "cod": "74909", "desc": "OTRAS ACTIVIDADES PROFESIONALES, CIENTÍFICAS Y TÉCNICAS N.C.P." }
  ]
}
```

**`GET /api/v1/environments/test`** (si ya configurado):

```json
{
  "success": true,
  "data": {
    "num_timbrado": "06038964",
    "fecha_inicio_vigencia": "2026-07-09",
    "id_csc": "0001",
    "key_version": 1,
    "has_csc": true
  }
}
```

**`PUT /api/v1/kude-config`:**

```json
{
  "template_id": "minimalista",
  "color_primario": "#0f172a",
  "notas_footer": "Gracias por su compra",
  "mostrar_fantasia": 1
}
```

`mostrar_fantasia`: `1` muestra el nombre de fantasía en el encabezado del KuDE (si está cargado en Empresa); `0` solo razón social.

---

## 5. Secretos vía Go (panel, solo owner)

El navegador **no** cifra con `ESIGN_MASTER_KEY`; envía datos en claro a Go y Go persiste cifrado en Oracle.

### Certificado `.p12`

**URL:** `{GO}/v1/panel/certificate` · **POST** · JWT owner

```json
{
  "p12_base64": "<contenido del .p12 en base64>",
  "password": "password-del-p12"
}
```

### Timbrado + CSC

**URL:** `{GO}/v1/panel/environments` · **PUT** · JWT owner

```json
{
  "environment": "TEST",
  "num_timbrado": "06038964",
  "fecha_inicio_vigencia": "2026-07-09",
  "id_csc": "0001",
  "csc": "ABCD0000000000000000000000000000"
}
```

---

## 6. Plano interno (solo Go)

Header: `X-Service-Token: <SERVICE_TOKEN>`  
Base: `{ORDS}/internal/v1`

Endpoints principales: `/context`, `/next-number`, `/certificate`, `/csc`, `/documents`, `/events`, `/logs`.

No usar desde integradores externos. Detalle en [`api-contract.md`](./api-contract.md) §5.

---

## 7. Errores frecuentes

| `error.code` | HTTP | Causa típica |
|---|---|---|
| `INVALID_KEY` | 401 | API key inexistente o mal copiada |
| `INVALID_KEY_PREFIX` | 401 | Key sin prefijo `sk_test_` / `sk_prod_` |
| `FORBIDDEN` | 403 | Rol sin permiso (ej. `analyst` en certificado) |
| `NO_CERTIFICATE` | 422 | Falta subir P12 antes de emitir |
| `VALIDATION_ERROR` | 422 | JSON válido pero datos SIFEN inconsistentes |
| `SEND_ERROR` | 502 | Firmado pero WS SIFEN no respondió → estado `FIRMADO` |

---

## 8. Herramientas

- **Swagger UI (integradores):** importar [`openapi-emision.yaml`](./openapi-emision.yaml).
- **Tipos TypeScript (panel esign):** desde el repo `esign`, `npm run generate:api` (lee [`openapi-panel.yaml`](./openapi-panel.yaml)).
- **Pruebas SIFEN:** usar siempre `sk_test_…` contra homologación; timbrado/CSC de prueba según la regla del proyecto `sifen-valores-test-prod`.
