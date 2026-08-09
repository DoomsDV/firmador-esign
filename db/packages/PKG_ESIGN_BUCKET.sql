-- PKG_ESIGN_BUCKET: subida/borrado de objetos en OCI Object Storage via
-- apex_web_service.make_rest_request + Web Credential OCI_BUCKET_CRED.
-- Requiere sesion APEX (create_session app 104 / page 1 / user ESIGN) o llamarse
-- desde un proceso APEX. Parametros: OCI_BUCKET_BASE_URL, OCI_CREDENTIAL_NAME.
CREATE OR REPLACE PACKAGE pkg_esign_bucket AS

  FUNCTION fn_base_url RETURN VARCHAR2;
  FUNCTION fn_credential RETURN VARCHAR2;

  -- Sube un BLOB. p_object_name es la clave relativa (ej. clients/1/logo.png).
  -- Devuelve la URL publica del objeto.
  FUNCTION fn_put_object(
    p_object_name IN VARCHAR2,
    p_blob        IN BLOB,
    p_mime_type   IN VARCHAR2 DEFAULT 'application/octet-stream'
  ) RETURN VARCHAR2;

  PROCEDURE pr_put_object(
    p_object_name IN VARCHAR2,
    p_blob        IN BLOB,
    p_mime_type   IN VARCHAR2 DEFAULT 'application/octet-stream',
    po_url        OUT VARCHAR2,
    po_status     OUT NUMBER
  );

  PROCEDURE pr_delete_object(
    p_object_name IN VARCHAR2,
    po_status     OUT NUMBER
  );

  -- Asegura sesion APEX para poder resolver la web credential del workspace ESIGN.
  PROCEDURE pr_ensure_apex_session;

END pkg_esign_bucket;
/

CREATE OR REPLACE PACKAGE BODY pkg_esign_bucket AS

  c_app_id   CONSTANT NUMBER := 104;
  c_page_id  CONSTANT NUMBER := 1;
  c_username CONSTANT VARCHAR2(30) := 'ESIGN';

  FUNCTION fn_base_url RETURN VARCHAR2 IS
  BEGIN
    RETURN RTRIM(pkg_esign_util.fn_get_parameter('OCI_BUCKET_BASE_URL'), '/');
  END fn_base_url;

  FUNCTION fn_credential RETURN VARCHAR2 IS
  BEGIN
    RETURN NVL(pkg_esign_util.fn_get_parameter('OCI_CREDENTIAL_NAME'), 'OCI_BUCKET_CRED');
  END fn_credential;

  FUNCTION fn_safe_object_name(p_object_name IN VARCHAR2) RETURN VARCHAR2 IS
    l_name VARCHAR2(1000);
  BEGIN
    l_name := LTRIM(NVL(TRIM(p_object_name), ''), '/');
    IF l_name IS NULL THEN
      raise_application_error(-20010, 'object name required');
    END IF;
    IF INSTR(l_name, '..') > 0 THEN
      raise_application_error(-20011, 'invalid object name');
    END IF;
    RETURN l_name;
  END fn_safe_object_name;

  FUNCTION fn_object_url(p_object_name IN VARCHAR2) RETURN VARCHAR2 IS
    l_base VARCHAR2(1000) := fn_base_url;
  BEGIN
    IF l_base IS NULL THEN
      raise_application_error(-20012, 'OCI_BUCKET_BASE_URL not configured');
    END IF;
    RETURN l_base || '/' || fn_safe_object_name(p_object_name);
  END fn_object_url;

  PROCEDURE pr_ensure_apex_session IS
  BEGIN
    -- Si ya hay sesion APEX valida para app 104, no recrear.
    IF NVL(v('APP_ID'), 0) = c_app_id AND v('APP_USER') IS NOT NULL THEN
      RETURN;
    END IF;

    apex_util.set_workspace('ESIGN');
    apex_session.create_session(
      p_app_id   => c_app_id,
      p_page_id  => c_page_id,
      p_username => c_username
    );
  END pr_ensure_apex_session;

  PROCEDURE pr_put_via_bridge(
    p_object_name IN VARCHAR2,
    p_blob        IN BLOB,
    p_mime_type   IN VARCHAR2,
    po_url        OUT VARCHAR2,
    po_status     OUT NUMBER
  ) IS
  BEGIN
    -- Reutiliza OCI_BUCKET_CRED del workspace AOXDEV (misma tenancy/bucket ACL).
    aoxdev.pkg_oci_bridge.pr_put_object(
      p_base_url    => fn_base_url,
      p_object_name => fn_safe_object_name(p_object_name),
      p_blob        => p_blob,
      p_mime_type   => NVL(p_mime_type, 'application/octet-stream'),
      po_url        => po_url,
      po_status     => po_status
    );
  END pr_put_via_bridge;

  PROCEDURE pr_put_object(
    p_object_name IN VARCHAR2,
    p_blob        IN BLOB,
    p_mime_type   IN VARCHAR2 DEFAULT 'application/octet-stream',
    po_url        OUT VARCHAR2,
    po_status     OUT NUMBER
  ) IS
    l_resp CLOB;
    l_url  VARCHAR2(2000);
  BEGIN
    IF p_blob IS NULL OR DBMS_LOB.getlength(p_blob) = 0 THEN
      raise_application_error(-20013, 'empty blob');
    END IF;

    l_url := fn_object_url(p_object_name);
    pr_ensure_apex_session;

    apex_web_service.g_request_headers.delete;
    apex_web_service.g_request_headers(1).name  := 'Content-Type';
    apex_web_service.g_request_headers(1).value := NVL(p_mime_type, 'application/octet-stream');

    l_resp := apex_web_service.make_rest_request(
      p_url                  => l_url,
      p_http_method          => 'PUT',
      p_credential_static_id => fn_credential,
      p_body_blob            => p_blob
    );

    po_status := apex_web_service.g_status_code;
    IF po_status IN (401, 403) THEN
      -- Credencial ESIGN aun sin secretos OCI: fallback al bridge AOXDEV.
      pr_put_via_bridge(p_object_name, p_blob, p_mime_type, po_url, po_status);
      RETURN;
    END IF;

    IF po_status NOT BETWEEN 200 AND 299 THEN
      raise_application_error(
        -20014,
        'oci put failed http=' || po_status || ' body=' || SUBSTR(NVL(l_resp, ''), 1, 300)
      );
    END IF;

    po_url := l_url;
  END pr_put_object;

  FUNCTION fn_put_object(
    p_object_name IN VARCHAR2,
    p_blob        IN BLOB,
    p_mime_type   IN VARCHAR2 DEFAULT 'application/octet-stream'
  ) RETURN VARCHAR2 IS
    l_url    VARCHAR2(2000);
    l_status NUMBER;
  BEGIN
    pr_put_object(p_object_name, p_blob, p_mime_type, l_url, l_status);
    RETURN l_url;
  END fn_put_object;

  PROCEDURE pr_delete_object(
    p_object_name IN VARCHAR2,
    po_status     OUT NUMBER
  ) IS
    l_resp CLOB;
    l_url  VARCHAR2(2000);
  BEGIN
    l_url := fn_object_url(p_object_name);
    pr_ensure_apex_session;

    apex_web_service.g_request_headers.delete;
    l_resp := apex_web_service.make_rest_request(
      p_url                  => l_url,
      p_http_method          => 'DELETE',
      p_credential_static_id => fn_credential
    );

    po_status := apex_web_service.g_status_code;
    IF po_status IN (401, 403) THEN
      aoxdev.pkg_oci_bridge.pr_delete_object(
        p_base_url    => fn_base_url,
        p_object_name => fn_safe_object_name(p_object_name),
        po_status     => po_status
      );
      RETURN;
    END IF;

    -- 200/204 ok; 404 ya no existia
    IF po_status NOT IN (200, 204, 404) THEN
      raise_application_error(
        -20015,
        'oci delete failed http=' || po_status || ' body=' || SUBSTR(NVL(l_resp, ''), 1, 300)
      );
    END IF;
  END pr_delete_object;

END pkg_esign_bucket;
/
