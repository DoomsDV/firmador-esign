-- install_all.sql — instalador del esquema esign (Oracle ADB).
-- Orden: tablas -> paquete de sesion -> contexto -> funciones de politica -> resto de paquetes
--        -> aplicar VPD -> modulos ORDS -> seeds. Ejecutar conectado como el usuario ESIGN.
-- Requiere GRANTs previos: EXECUTE ON DBMS_CRYPTO, DBMS_SESSION, DBMS_RLS; y APEX_JWT accesible.
SET DEFINE OFF
SET SERVEROUTPUT ON
WHENEVER SQLERROR CONTINUE

PROMPT === Tablas ===
@@tables/01_app_parameter.sql
@@tables/02_users.sql
@@tables/03_client.sql
@@tables/04_client_user.sql
@@tables/05_user_session.sql
@@tables/06_client_emisor.sql
@@tables/07_client_emisor_actividad.sql
@@tables/08_client_establecimiento.sql
@@tables/09_client_punto_expedicion.sql
@@tables/10_client_sifen_env.sql
@@tables/11_document_sequence.sql
@@tables/12_document.sql
@@tables/13_document_xml.sql
@@tables/14_document_event.sql
@@tables/15_api_log.sql
@@tables/16_client_api_key.sql
@@tables/17_client_certificate.sql
@@tables/18_client_invitation.sql
@@tables/19_client_kude_config.sql

PROMPT === Paquete de sesion (requerido por el contexto) ===
@@packages/PKG_ESIGN_SESSION.sql

PROMPT === Contexto de aplicacion + funciones de politica ===
@@policies/01_context.sql
@@policies/02_fn_tenant_policy.sql

PROMPT === Paquetes API ===
-- PKG_ESIGN_BUCKET/PKG_ESIGN_KUDE_API van antes de PKG_ESIGN_CLIENT_API: este ultimo
-- llama a pkg_esign_kude_api.fn_config_json() desde pr_get_internal_context.
@@packages/PKG_ESIGN_HTTP.sql
@@packages/PKG_ESIGN_UTIL.sql
@@packages/PKG_ESIGN_BUCKET.sql
@@packages/PKG_ESIGN_KUDE_API.sql
@@packages/PKG_ESIGN_JWT.sql
@@packages/PKG_ESIGN_AUTH_API.sql
@@packages/PKG_ESIGN_CLIENT_API.sql
@@packages/PKG_ESIGN_APIKEY_API.sql
@@packages/PKG_ESIGN_CERT_API.sql
@@packages/PKG_ESIGN_DOCUMENT_API.sql

PROMPT === Aplicar politicas VPD (DBMS_RLS) ===
@@policies/03_apply_policies.sql

PROMPT === Modulos ORDS ===
@@ords/01_esign_module.sql
@@ords/02_esign_internal_module.sql

PROMPT === Seeds ===
@@seeds/01_app_parameter.sql

PROMPT === Instalacion finalizada ===
