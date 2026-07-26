-- PKG_ESIGN_APIKEY_API: API keys por ambiente (patron Stripe), respaldadas por la tabla hija
-- client_api_key (rotables, con historial). La key completa se muestra UNA sola vez al rotarla;
-- se persiste solo hash + prefijo. El ambiente lo determina el prefijo (sk_test_/sk_prod_).
-- pr_resolve_key es interno (Go): hash -> client + environment + status.
CREATE OR REPLACE PACKAGE pkg_esign_apikey_api AS

  PROCEDURE pr_rotate_key(p_client_id IN NUMBER, p_role IN VARCHAR2, p_environment IN VARCHAR2, p_out OUT CLOB);
  PROCEDURE pr_get_keys_meta(p_client_id IN NUMBER, p_out OUT CLOB);

  -- Interno (seed/carga): registra una key con hash + prefijo YA calculados en Go. Revoca las
  -- keys ACTIVE previas del mismo ambiente e inserta la nueva como ACTIVE (rotacion idempotente).
  PROCEDURE pr_register_key(
    p_client_id   IN NUMBER,
    p_environment IN VARCHAR2,
    p_prefix      IN VARCHAR2,
    p_hash        IN VARCHAR2,
    p_label       IN VARCHAR2 DEFAULT NULL
  );

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

  -- assert_env valida el ambiente (TEST/PROD).
  PROCEDURE assert_env(p_env IN VARCHAR2) IS
  BEGIN
    IF p_env NOT IN ('TEST','PROD') THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request, 'environment debe ser TEST o PROD');
    END IF;
  END assert_env;

  -- revoke_active revoca las keys ACTIVE del ambiente (previo a insertar la nueva).
  PROCEDURE revoke_active(p_client_id IN NUMBER, p_env IN VARCHAR2) IS
  BEGIN
    UPDATE /*+ no_parallel */ client_api_key
       SET status = 'REVOKED', revoked_at = CURRENT_TIMESTAMP
     WHERE client_id = p_client_id AND environment = p_env AND status = 'ACTIVE';
  END revoke_active;

  PROCEDURE pr_register_key(
    p_client_id   IN NUMBER,
    p_environment IN VARCHAR2,
    p_prefix      IN VARCHAR2,
    p_hash        IN VARCHAR2,
    p_label       IN VARCHAR2 DEFAULT NULL
  ) IS
    l_env VARCHAR2(4) := UPPER(p_environment);
  BEGIN
    assert_env(l_env);
    pkg_esign_session.set_client(p_client_id);
    revoke_active(p_client_id, l_env);
    INSERT /*+ no_parallel */ INTO client_api_key (client_id, environment, prefix, key_hash, label, status)
    VALUES (p_client_id, l_env, p_prefix, p_hash, p_label, 'ACTIVE');
  END pr_register_key;

  PROCEDURE pr_rotate_key(p_client_id IN NUMBER, p_role IN VARCHAR2, p_environment IN VARCHAR2, p_out OUT CLOB) IS
    l_env    VARCHAR2(4) := UPPER(p_environment);
    l_secret VARCHAR2(80);
    l_key    VARCHAR2(100);
    l_prefix VARCHAR2(24);
    l_hash   VARCHAR2(128);
    l_label  VARCHAR2(100);
  BEGIN
    assert_owner_or_dev(p_role);
    assert_env(l_env);
    pkg_esign_session.set_client(p_client_id);

    l_secret := pkg_esign_util.fn_random_hex(24); -- 48 hex chars
    l_key    := CASE l_env WHEN 'TEST' THEN 'sk_test_' ELSE 'sk_prod_' END || l_secret;
    l_prefix := SUBSTR(l_key, 1, 12); -- p.ej. sk_test_a1b2
    l_hash   := pkg_esign_util.fn_hash_api_key(l_key);

    revoke_active(p_client_id, l_env);
    INSERT /*+ no_parallel */ INTO client_api_key (client_id, environment, prefix, key_hash, label, status)
    VALUES (p_client_id, l_env, l_prefix, l_hash, l_label, 'ACTIVE');

    -- La key completa se devuelve SOLO ahora; no se vuelve a exponer.
    DECLARE l_data CLOB; BEGIN
      SELECT JSON_OBJECT('environment' VALUE l_env, 'api_key' VALUE l_key, 'prefix' VALUE l_prefix RETURNING CLOB)
        INTO l_data FROM dual;
      p_out := pkg_esign_util.fn_ok(l_data);
    END;
  END pr_rotate_key;

  PROCEDURE pr_get_keys_meta(p_client_id IN NUMBER, p_out OUT CLOB) IS
    l_keys CLOB;
    l_data CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);
    SELECT JSON_ARRAYAGG(
             JSON_OBJECT('environment' VALUE environment, 'prefix' VALUE prefix,
                         'status' VALUE status, 'label' VALUE label,
                         'created_at' VALUE TO_CHAR(created_at, 'YYYY-MM-DD"T"HH24:MI:SSTZH:TZM')
                         RETURNING CLOB)
             ORDER BY environment, created_at DESC RETURNING CLOB)
      INTO l_keys
      FROM client_api_key WHERE client_id = p_client_id;
    l_keys := NVL(l_keys, TO_CLOB('[]'));
    SELECT JSON_OBJECT('keys' VALUE l_keys FORMAT JSON RETURNING CLOB) INTO l_data FROM dual;
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
    IF NOT (REGEXP_LIKE(p_api_key, '^sk_test_') OR REGEXP_LIKE(p_api_key, '^sk_prod_')) THEN
      raise_application_error(pkg_esign_http.c_ora_unauthorized, 'prefijo de API key invalido');
    END IF;

    -- Lookup pre-auth (aun no hay tenant en el contexto): bypass acotado de la VPD.
    pkg_esign_session.begin_bootstrap;
    SELECT client_id, environment, status
      INTO p_client_id, p_environment, p_status
      FROM client_api_key WHERE key_hash = l_hash;
    pkg_esign_session.end_bootstrap;
  EXCEPTION
    WHEN NO_DATA_FOUND THEN
      pkg_esign_session.end_bootstrap;
      raise_application_error(pkg_esign_http.c_ora_unauthorized, 'API key invalida');
  END pr_resolve_key;

END pkg_esign_apikey_api;
/
