# Acta E2E SIFEN-test staging — 2026-08-31 / 2026-09-01

## Resumen ejecutivo
Dos corridas aisladas sobre org fixture **29** (QA Billing E2E), ambiente **SIFEN-test** / `api-staging.etick.uno`, sin tocar producción:

1. **Corrida A (invoice 5)** — circuito feliz completo hasta email SENT.
2. **Corrida B (invoice 6)** — emisión única + fallo reversible pre-`apex_mail.push_queue` (billing_email NULL acotado a org 29) + restore + **solo** `pr_retry_pending_einvoice_emails` → exactamente un email SENT.

Invoice 5 / CDC `…5640` no se modificó en la corrida B.

---

## Corrida A — bridge feliz (cerrada)

| Campo | Valor |
|---|---|
| Invoice | **5** |
| emission_key / outbox | `INV-5` / DONE attempts=**1** |
| CDC | `01060389648001001000001912026083113451035640` |
| SIFEN | APROBADO / **0260** / protAut **49936116** |
| XML SHA-256 | `6238714727b22db4d67adb7750707c76af9f6ec6639fb24728da16420f0901d6` |
| XML size | 8837 |
| KuDE | READY → OCI `.../kude/test/4/<CDC>.pdf` |
| Email | **SENT** → `mike.sdk83@gmail.com` |
| mail_id | `11453742397199053` |
| sent_at | `2026-09-01 01:18:11 +00:00` |
| email_attempts | 1 |

### Infra / contrato (desbloqueos previos a A)
- VPS staging: `ESIGN_KUDE_QUEUE_ENABLED=true`, `GOTENBERG_URL=http://172.17.0.4:3000` (solo contenedor staging).
- AOXDEV ORDS: bind `X-Service-Token` → `:service_token` + `:status_code` (`migrations/20260901_einvoice_ords_service_token.sql`). Bookmate poll-kude apunta a Hasel `/internal/subscription-invoices/*`, **no** a `esign_internal`.
- Bookmate staging: Array.isArray + rechazo logical-error en poll-kude.

---

## Corrida B — mail-recovery (autorizada)

| Campo | Valor |
|---|---|
| Invoice | **6** (nueva; no reutilizar 5) |
| Trigger | `pr_run_billing_cycle_for_org(29)` tras crédito + `current_period_end` vencido + purge idempotency `CYCLE:29:YYYYMMDD` |
| emission_key / outbox | `INV-6` / DONE attempts=**1** (sin redespacho) |
| CDC | `01060389648001001000002012026083117532268213` |
| SIFEN | APROBADO / **0260** / protAut **49936137** |
| XML SHA-256 | `41e6d16118b22b8cb7aff5b16d6bb11562842d77f8cf69ad72c07a3713305e3d` |
| XML size | 8847 |
| KuDE READY | `2026-09-01 01:26:17` → OCI `.../17532268213.pdf` |

### Fallo inducido (reversible, acotado a org QA 29)
1. Tras KuDE READY: `UPDATE org_billing_profile SET billing_email = NULL WHERE org_id_organization = 29`.
2. `GET staging.hasel.app/api/internal/esign/poll-kude` → artefacts persistidos; email **no** enviado.

**Evidencia intento fallido (invoice 6):**
| Check | Resultado |
|---|---|
| `einvoice_xml_firmado` / SHA / size | SET / `41e6…5e3d` / 8847 |
| `einvoice_kude_url` | SET (URL OCI del CDC nuevo) |
| `einvoice_email_status` | **FAILED** |
| `einvoice_email_error` | `Faltan artefactos o billing_email` |
| `einvoice_email_mail_id` | **NULL** |
| `einvoice_sent_at` | **NULL** |
| `einvoice_email_attempts` | 1 |
| outbox | DONE attempts=**1** (sin reemitir FE) |

El fallo ocurre en `pr_send_einvoice_email` **después** del `COMMIT` de artefactos y **antes** de `apex_mail.send` / `push_queue` (guard de `billing_email`).

### Restore + retry
1. `billing_email` restaurado a `mike.sdk83@gmail.com`.
2. `pkg_aox_subscription_billing_api.pr_retry_pending_einvoice_emails(10, 29)` — **sin** outbox/emit-invoice/SIFEN.

**Evidencia post-retry (invoice 6):**
| Check | Resultado |
|---|---|
| email_status | **SENT** |
| email_to | `mike.sdk83@gmail.com` |
| mail_id | `11454010871252881` |
| sent_at | `2026-09-01 01:26:23 +00:00` |
| email_attempts | **2** (1 fallo + 1 éxito) |
| CDC / SHA / KuDE URL | **idénticos** al intento fallido |
| outbox | sigue DONE attempts=**1** |
| Invoice 5 | intacta (mismo mail_id/SHA/CDC) |

Adjuntos esperados por código (`pr_send_einvoice_email`): un `factura-{CDC}.xml` + un `factura-{CDC}.pdf` y un solo `apex_mail.push_queue` por envío exitoso.

---

## Invariantes respetados
- No producción Firmador / no contenedor prod VPS.
- No reemisión de invoice 5; corrida B = una sola FE nueva (CDC distinto).
- Outbox por invoice: un solo dispatch DONE.
- Fallo email sin código de aplicación: solo DML reversible en `org_billing_profile` de org 29.

## Cambios / deploys (estado)
| Ítem | Estado |
|---|---|
| AOXDEV ORDS X-Service-Token bind | **Desplegado** en aoxdev (+ fuentes migraciones actualizadas) |
| Bookmate poll-kude hardening | **En staging** (previo) |
| firmador `condicion.go` duplicate case 19 | **Corregido en repo** (19/20/21/99); **sin** deploy prod |
| GOTENBERG_URL por IP Docker | Staging OK; frágil si recrean contenedor |

## Evidencia en disco
`C:\Users\HP\Desktop\firmador\_e2e_evidence\`
- `ACTA_E2E.md` (este archivo)
- `mail_recovery_run2.json`
- `bridge_mail_result.json` / `kude_ready.json` (corrida A)
- `xml_canonical.xml` (corrida A)

## Cierre de evidencia (sin nuevas emisiones) — 2026-09-01 ~01:27 UTC+

### Restauración
| Check | Fuente | Resultado |
|---|---|---|
| `org_billing_profile.billing_email` org 29 | **Estado de sistema** | `mike.sdk83@gmail.com` (restaurado; no quedó NULL) |

### Invoice 6 — inmutables / ant-duplicado
| Check | Fuente | Resultado |
|---|---|---|
| CDC | **Estado de sistema** | `01060389648001001000002012026083117532268213` |
| SHA-256 | **Estado de sistema** | `41e6d16118b22b8cb7aff5b16d6bb11562842d77f8cf69ad72c07a3713305e3d` |
| KuDE URL | **Estado de sistema** | `.../kude/test/4/<CDC>.pdf` (mismo path que tras FAILED) |
| Outbox | **Estado de sistema** | `INV-6` DONE **attempts=1**, lease vacío |
| Email final | **Estado de sistema** | SENT · `mail_id=11454010871252881` · attempts=2 · lease NULL · `sent_at` set |
| Elegibles `pr_retry…` org 29 | **Estado de sistema** | **0** filas (no hay FAILED/NONE con artefactos y `sent_at` NULL) |
| `apex_mail_queue` mike.sdk83 | **Estado de sistema** | **0** (nada pendiente que reenvíe) |

### Adjuntos del correo final (invoice 6)
| Check | Fuente | Resultado |
|---|---|---|
| Un solo envío APEX al buzón QA post-01:26 | **Estado de sistema** (`apex_mail_log`) | 1 fila `mail_id=11454010871252881` · to=`mike.sdk83@gmail.com` · subj factura suscripción · `mail_send_error` NULL · send_end `01:26:24` |
| Cantidad de adjuntos | **Estado de sistema** (`apex_mail_log.mail_attachment_count`) | **2** |
| Nombres `factura-{CDC}.xml` / `.pdf` | **Por diseño de código** (`pr_send_einvoice_email` añade exactamente esos dos filenames antes de un `push_queue`) + coherencia con count=2 | **No re-leído el BLOB post-cola** (vista `apex_mail_attachments` vacía tras envío; sin privilege a tabla interna WWV). Nombres no observados en buzón IMAP en este cierre. |
| Buzón Gmail (contenido visual) | **No observado** en este cierre | Pendiente opcional humano |

También en `apex_mail_log` (mismo buzón, misma ventana): exactamente **2** mails Hasel FE — invoice 5 (`…99053`, count=2) e invoice 6 (`…52881`, count=2). Sin terceros.

### Invoice 5 — sin cambios
| Check | Fuente | Resultado |
|---|---|---|
| CDC / SHA / mail_id / attempts / outbox | **Estado de sistema** | Igual a corrida A: CDC `…5640`, SHA `6238…01d6`, mail `…99053`, email_attempts=1, outbox DONE attempts=1 |

### Todos del plan
Todos los ítems del plan E2E quedaron **completed** (preflight, emisión, bridge, mail-recovery, evidence-closeout / acta dual).

### Git sin commit (solo inventario; no push)
**firmador** (`staging`):  
`M` `db/install_all.sql`, `db/ords/02_esign_internal_module.sql`, `db/packages/PKG_ESIGN_KUDE_TASK_API.sql`, `internal/httpapi/build.go`, `internal/sifen/condicion.go`, `internal/tenant/ords_xml_kude_test.go`  
`??` `_e2e_evidence/`, scripts `_e2e_*`, `db/migrations/20260901_ords_internal_service_token.sql`

**bookmate/bookmate** (`staging`):  
`M` `src/lib/esign.ts`, `src/pages/api/internal/esign/poll-kude.ts`

**aox-dev** (`main`):  
`M` `migrations/20260810_subscription_einvoice_ords.sql`, `migrations/20260831_einvoice_artifacts_ords.sql`, `packages/PKG_AOX_SUBSCRIPTION_BILLING_API.pls`, `tables/ORG_SUBSCRIPTION_INVOICE.sql`  
`??` `migrations/20260901_einvoice_ords_service_token.sql`

## Veredicto final
**PASS** del escenario E2E solicitado (corrida A + mail-recovery en corrida B), con restauración de `billing_email` verificada y sin riesgo de reenvío automático pendiente en org 29. Única salvedad de evidencia: **nombres exactos de adjuntos** acreditados por código + `mail_attachment_count=2`, no por lectura post-hoc del buzón/BLOB.
