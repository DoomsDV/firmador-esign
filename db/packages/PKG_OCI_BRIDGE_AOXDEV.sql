-- Bridge DEFINER en AOXDEV: permite a ESIGN subir a bucket-etick-dev reutilizando
-- la Web Credential OCI_BUCKET_CRED ya configurada en el workspace AOXDEV.
-- Uso temporal hasta completar set_persistent_credentials en ESIGN.
CREATE OR REPLACE PACKAGE pkg_oci_bridge AUTHID DEFINER AS
  PROCEDURE pr_put_object(
    p_base_url    IN VARCHAR2,
    p_object_name IN VARCHAR2,
    p_blob        IN BLOB,
    p_mime_type   IN VARCHAR2,
    po_url        OUT VARCHAR2,
    po_status     OUT NUMBER
  );

  PROCEDURE pr_delete_object(
    p_base_url    IN VARCHAR2,
    p_object_name IN VARCHAR2,
    po_status     OUT NUMBER
  );
END pkg_oci_bridge;
/

CREATE OR REPLACE PACKAGE BODY pkg_oci_bridge AS

  c_credential CONSTANT VARCHAR2(50) := 'OCI_BUCKET_CRED';

  FUNCTION fn_url(p_base_url IN VARCHAR2, p_object_name IN VARCHAR2) RETURN VARCHAR2 IS
    l_base VARCHAR2(1000) := RTRIM(TRIM(p_base_url), '/');
    l_obj  VARCHAR2(1000) := LTRIM(TRIM(p_object_name), '/');
  BEGIN
    IF l_base IS NULL OR l_obj IS NULL THEN
      raise_application_error(-20100, 'base_url and object_name required');
    END IF;
    RETURN l_base || '/' || l_obj;
  END fn_url;

  PROCEDURE pr_ensure_session IS
    l_app NUMBER;
  BEGIN
    IF NVL(v('APP_ID'), 0) > 0 AND v('APP_USER') IS NOT NULL THEN
      -- Ya hay sesion; si no es AOXDEV igual recreamos para usar su credential.
      NULL;
    END IF;
    SELECT MIN(application_id) INTO l_app
      FROM apex_applications
     WHERE workspace = 'AOXDEV';
    IF l_app IS NULL THEN
      raise_application_error(-20101, 'no apex app in AOXDEV workspace');
    END IF;
    apex_util.set_workspace('AOXDEV');
    apex_session.create_session(
      p_app_id   => l_app,
      p_page_id  => 1,
      p_username => 'AOXDEV'
    );
  END pr_ensure_session;

  PROCEDURE pr_put_object(
    p_base_url    IN VARCHAR2,
    p_object_name IN VARCHAR2,
    p_blob        IN BLOB,
    p_mime_type   IN VARCHAR2,
    po_url        OUT VARCHAR2,
    po_status     OUT NUMBER
  ) IS
    l_resp CLOB;
    l_url  VARCHAR2(2000);
  BEGIN
    IF p_blob IS NULL OR DBMS_LOB.getlength(p_blob) = 0 THEN
      raise_application_error(-20102, 'empty blob');
    END IF;
    l_url := fn_url(p_base_url, p_object_name);
    pr_ensure_session;

    apex_web_service.g_request_headers.delete;
    apex_web_service.g_request_headers(1).name  := 'Content-Type';
    apex_web_service.g_request_headers(1).value := NVL(p_mime_type, 'application/octet-stream');

    l_resp := apex_web_service.make_rest_request(
      p_url                  => l_url,
      p_http_method          => 'PUT',
      p_credential_static_id => c_credential,
      p_body_blob            => p_blob
    );
    po_status := apex_web_service.g_status_code;
    IF po_status NOT BETWEEN 200 AND 299 THEN
      raise_application_error(-20103, 'oci put http=' || po_status || ' ' || SUBSTR(NVL(l_resp,''),1,300));
    END IF;
    po_url := l_url;
  END pr_put_object;

  PROCEDURE pr_delete_object(
    p_base_url    IN VARCHAR2,
    p_object_name IN VARCHAR2,
    po_status     OUT NUMBER
  ) IS
    l_resp CLOB;
    l_url  VARCHAR2(2000);
  BEGIN
    l_url := fn_url(p_base_url, p_object_name);
    pr_ensure_session;
    apex_web_service.g_request_headers.delete;
    l_resp := apex_web_service.make_rest_request(
      p_url                  => l_url,
      p_http_method          => 'DELETE',
      p_credential_static_id => c_credential
    );
    po_status := apex_web_service.g_status_code;
    IF po_status NOT IN (200, 204, 404) THEN
      raise_application_error(-20104, 'oci delete http=' || po_status || ' ' || SUBSTR(NVL(l_resp,''),1,300));
    END IF;
  END pr_delete_object;

END pkg_oci_bridge;
/

GRANT EXECUTE ON pkg_oci_bridge TO esign;
