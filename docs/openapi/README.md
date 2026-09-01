# OpenAPI — índice de especificaciones esign

La API de la plataforma se documenta en **tres contratos OpenAPI independientes**, uno por plano.

## Qué spec usar

| Audiencia | Archivo | Base URL | Auth |
|---|---|---|---|
| **Integradores** (ERP, e-commerce) | [`openapi-emision.yaml`](../openapi-emision.yaml) | `{GO}/v1` | `Authorization: Bearer sk_test_…` / `sk_prod_…` |
| **Panel web** (frontend esign) | [`openapi-panel.yaml`](../openapi-panel.yaml) | `{ORDS}/api/v1` + `{GO}/v1/panel` | JWT del panel |
| **Servicio Go** (privado) | [`openapi-internal.yaml`](../openapi-internal.yaml) | `{ORDS}/internal/v1` | `X-Service-Token` |

Documentación complementaria:

- Guía práctica integradores: [`guia-integracion-api.md`](../guia-integracion-api.md)
- Contrato narrativo completo: [`api-contract.md`](../api-contract.md)

## Importar en Swagger UI / Redoc

No importes el monolito antiguo. Usa el YAML del plano que corresponda:

```text
# Integradores — emisión SIFEN
docs/openapi-emision.yaml

# Panel esign — ORDS + mediación Go
docs/openapi-panel.yaml

# Interno Go↔ORDS (no publicar)
docs/openapi-internal.yaml
```

## Tipos TypeScript (panel esign)

Desde el repo `esign`:

```bash
npm run generate:api
```

Genera tipos desde `openapi-panel.yaml` (no incluye emisión ni interno).

## Mantenimiento

Cada spec es **autocontenida** (sin `$ref` cruzados entre archivos). Al cambiar un endpoint:

1. Editar **solo** la spec del plano afectado.
2. Si el cambio es transversal (p. ej. el envelope `{ success, data, error }`), replicarlo en las tres specs.

## Deprecación

El archivo [`openapi.yaml`](../openapi.yaml) ya **no es una spec ejecutable**. Solo redirige a este índice. No lo importes en Swagger UI.
