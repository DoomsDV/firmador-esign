-- PKG_ESIGN_APIKEY_API: API keys por ambiente (patron Stripe). La key completa se muestra
-- UNA sola vez al rotarla; se persiste solo hash + prefijo. El ambiente lo determina el
-- prefijo (sk_test_ / sk_prod_). pr_resolve_key es interno (Go): hash -> client + environment.
CREATE OR REPLACE PACKAGE pkg_esign_apikey_api AS

  PROCEDURE pr_rotate_key(p_client_id IN NUMBER, p_role IN VARCHAR2, p_environment IN VARCHAR2, p_out OUT CLOB);
  PROCEDURE pr_get_keys_meta(p_client_id IN NUMBER, p_out OUT CLOB);

  -- Interno (Go): resuelve la API key a client_id + environment + status.
  PROCEDURE pr_resolve_key(
    p_api_key     IN  VARCHAR2,
    p_client_id   OUT NUMBER,
    p_environment OUT VARCHAR2,
    p_status      OUT VARCHAR2
  );

END pkg_esign_apikey_api;
/

CREATE OR REPLACE PACKAGE BODY pkg_esign_apikey_api AS

  PROCEDURE assert_owner_or_dev(p_role IN VARCHAR2) IS
  BEGIN
    IF p_role NOT IN ('owner','developer') THEN
      raise_application_error(pkg_esign_http.c_ora_forbidden, 'solo owner/developer pueden gestionar API keys');
    END IF;
  END assert_owner_or_dev;

  PROCEDURE pr_rotate_key(p_client_id IN NUMBER, p_role IN VARCHAR2, p_environment IN VARCHAR2, p_out OUT CLOB) IS
    l_env    VARCHAR2(4) := UPPER(p_environment);
    l_secret VARCHAR2(80);
    l_key    VARCHAR2(100);
    l_prefix VARCHAR2(24);
    l_hash   VARCHAR2(128);
  BEGIN
    assert_owner_or_dev(p_role);
    pkg_esign_session.set_client(p_client_id);
    IF l_env NOT IN ('TEST','PROD') THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request, 'environment debe ser TEST o PROD');
    END IF;

    l_secret := pkg_esign_util.fn_random_hex(24); -- 48 hex chars
    l_key    := CASE l_env WHEN 'TEST' THEN 'sk_test_' ELSE 'sk_prod_' END || l_secret;
    l_prefix := SUBSTR(l_key, 1, 12); -- p.ej. sk_test_a1b2
    l_hash   := pkg_esign_util.fn_hash_api_key(l_key);

    IF l_env = 'TEST' THEN
      UPDATE client SET sk_test_prefix = l_prefix, sk_test_hash = l_hash, sk_test_status = 'ACTIVE'
       WHERE id_client = p_client_id;
    ELSE
      UPDATE client SET sk_prod_prefix = l_prefix, sk_prod_hash = l_hash, sk_prod_status = 'ACTIVE'
       WHERE id_client = p_client_id;
    END IF;

    -- La key completa se devuelve SOLO ahora; no se vuelve a exponer.
    DECLARE l_data CLOB; BEGIN
      SELECT JSON_OBJECT('environment' VALUE l_env, 'api_key' VALUE l_key, 'prefix' VALUE l_prefix RETURNING CLOB)
        INTO l_data FROM dual;
      p_out := pkg_esign_util.fn_ok(l_data);
    END;
  END pr_rotate_key;

  PROCEDURE pr_get_keys_meta(p_client_id IN NUMBER, p_out OUT CLOB) IS
    l_data CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);
    SELECT JSON_OBJECT(
             'test' VALUE JSON_OBJECT('prefix' VALUE sk_test_prefix, 'status' VALUE sk_test_status),
             'prod' VALUE JSON_OBJECT('prefix' VALUE sk_prod_prefix, 'status' VALUE sk_prod_status)
             RETURNING CLOB)
      INTO l_data FROM client WHERE id_client = p_client_id;
    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_get_keys_meta;

  PROCEDURE pr_resolve_key(
    p_api_key     IN  VARCHAR2,
    p_client_id   OUT NUMBER,
    p_environment OUT VARCHAR2,
    p_status      OUT VARCHAR2
  ) IS
    l_hash VARCHAR2(128) := pkg_esign_util.fn_hash_api_key(p_api_key);
  BEGIN
    -- El ambiente se deriva del prefijo de la key; se valida contra la columna correcta.
    pkg_esign_session.begin_bootstrap;
    IF REGEXP_LIKE(p_api_key, '^sk_test_') THEN
      p_environment := 'TEST';
      SELECT id_client, sk_test_status INTO p_client_id, p_status
        FROM client WHERE sk_test_hash = l_hash;
    ELSIF REGEXP_LIKE(p_api_key, '^sk_prod_') THEN
      p_environment := 'PROD';
      SELECT id_client, sk_prod_status INTO p_client_id, p_status
        FROM client WHERE sk_prod_hash = l_hash;
    ELSE
      pkg_esign_session.end_bootstrap;
      raise_application_error(pkg_esign_http.c_ora_unauthorized, 'prefijo de API key invalido');
    END IF;
    pkg_esign_session.end_bootstrap;
  EXCEPTION
    WHEN NO_DATA_FOUND THEN
      pkg_esign_session.end_bootstrap;
      raise_application_error(pkg_esign_http.c_ora_unauthorized, 'API key invalida');
  END pr_resolve_key;

END pkg_esign_apikey_api;
/
