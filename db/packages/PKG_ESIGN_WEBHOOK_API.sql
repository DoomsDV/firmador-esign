-- PKG_ESIGN_WEBHOOK_API: configuración de webhook por cliente/ambiente (panel + contexto interno).
CREATE OR REPLACE PACKAGE pkg_esign_webhook_api AS

  FUNCTION fn_config_json(p_client_id IN NUMBER, p_environment IN VARCHAR2) RETURN CLOB;

  PROCEDURE pr_get(
    p_client_id   IN NUMBER,
    p_role        IN VARCHAR2,
    p_environment IN VARCHAR2,
    p_out         OUT CLOB
  );

  PROCEDURE pr_upsert(
    p_client_id   IN NUMBER,
    p_role        IN VARCHAR2,
    p_environment IN VARCHAR2,
    p_body        IN CLOB,
    p_out         OUT CLOB
  );

  -- Interno (Go): persiste secret ya cifrado (hex). Rotación desde el panel vía Go.
  PROCEDURE pr_store_secret(
    p_client_id   IN NUMBER,
    p_environment IN VARCHAR2,
    p_body        IN CLOB,
    p_out         OUT CLOB
  );

  -- Interno (Go worker): secret cifrado para firmar HMAC.
  PROCEDURE pr_get_secret_json(
    p_client_id   IN NUMBER,
    p_environment IN VARCHAR2,
    p_out         OUT CLOB
  );

END pkg_esign_webhook_api;
/

CREATE OR REPLACE PACKAGE BODY pkg_esign_webhook_api AS

  PROCEDURE assert_owner(p_role IN VARCHAR2) IS
  BEGIN
    IF p_role IS NULL OR p_role <> 'owner' THEN
      raise_application_error(pkg_esign_http.c_ora_forbidden, 'solo el owner puede configurar webhooks');
    END IF;
  END assert_owner;

  FUNCTION fn_config_json(p_client_id IN NUMBER, p_environment IN VARCHAR2) RETURN CLOB IS
    l_env  VARCHAR2(4) := UPPER(p_environment);
    l_data CLOB;
  BEGIN
    IF l_env NOT IN ('TEST', 'PROD') THEN
      RETURN NULL;
    END IF;
    BEGIN
      SELECT JSON_OBJECT(
               'url' VALUE w.url,
               'is_active' VALUE w.is_active,
               'has_secret' VALUE CASE
                 WHEN w.secret_ciphertext IS NOT NULL AND DBMS_LOB.GETLENGTH(w.secret_ciphertext) > 0
                 THEN 'true' ELSE 'false' END FORMAT JSON,
               'key_version' VALUE w.key_version
               RETURNING CLOB)
        INTO l_data
        FROM client_webhook_endpoint w
       WHERE w.client_id = p_client_id AND w.environment = l_env;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN
        SELECT JSON_OBJECT(
                 'url' VALUE NULL,
                 'is_active' VALUE 0,
                 'has_secret' VALUE 'false' FORMAT JSON,
                 'key_version' VALUE 1
                 RETURNING CLOB)
          INTO l_data FROM dual;
    END;
    RETURN l_data;
  END fn_config_json;

  PROCEDURE pr_get(
    p_client_id   IN NUMBER,
    p_role        IN VARCHAR2,
    p_environment IN VARCHAR2,
    p_out         OUT CLOB
  ) IS
    l_env VARCHAR2(4) := UPPER(p_environment);
  BEGIN
    IF p_role NOT IN ('owner', 'developer') THEN
      raise_application_error(pkg_esign_http.c_ora_forbidden, 'rol no autorizado');
    END IF;
    IF l_env NOT IN ('TEST', 'PROD') THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request, 'environment debe ser TEST o PROD');
    END IF;
    pkg_esign_session.set_client(p_client_id);
    p_out := pkg_esign_util.fn_ok(fn_config_json(p_client_id, l_env));
  END pr_get;

  PROCEDURE pr_upsert(
    p_client_id   IN NUMBER,
    p_role        IN VARCHAR2,
    p_environment IN VARCHAR2,
    p_body        IN CLOB,
    p_out         OUT CLOB
  ) IS
    l_env      VARCHAR2(4) := UPPER(p_environment);
    l_url      VARCHAR2(500) := TRIM(json_value(p_body, '$.url'));
    l_active   NUMBER := CASE LOWER(NVL(json_value(p_body, '$.is_active'), 'false'))
                           WHEN 'true' THEN 1 WHEN '1' THEN 1 ELSE 0 END;
  BEGIN
    assert_owner(p_role);
    IF l_env NOT IN ('TEST', 'PROD') THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request, 'environment debe ser TEST o PROD');
    END IF;
    IF l_active = 1 AND (l_url IS NULL OR LENGTH(l_url) = 0) THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request, 'url requerida cuando el webhook está activo');
    END IF;
    IF l_url IS NOT NULL AND NOT REGEXP_LIKE(l_url, '^https://', 'i') THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request, 'url debe usar HTTPS');
    END IF;

    pkg_esign_session.set_client(p_client_id);

    MERGE INTO client_webhook_endpoint t
    USING (SELECT p_client_id AS cid, l_env AS env FROM dual) s
    ON (t.client_id = s.cid AND t.environment = s.env)
    WHEN MATCHED THEN UPDATE SET
      url = l_url,
      is_active = l_active,
      updated_at = SYSTIMESTAMP
    WHEN NOT MATCHED THEN INSERT
      (client_id, environment, url, is_active)
    VALUES
      (p_client_id, l_env, l_url, l_active);

    p_out := pkg_esign_util.fn_ok(fn_config_json(p_client_id, l_env));
  END pr_upsert;

  PROCEDURE pr_store_secret(
    p_client_id   IN NUMBER,
    p_environment IN VARCHAR2,
    p_body        IN CLOB,
    p_out         OUT CLOB
  ) IS
    l_env       VARCHAR2(4) := UPPER(p_environment);
    l_ct        BLOB;
    l_nonce     RAW(16);
    l_ct_hex    VARCHAR2(32767);
    l_nonce_hex VARCHAR2(64);
    l_ver       NUMBER := NVL(json_value(p_body, '$.key_version'), 1);
  BEGIN
    IF l_env NOT IN ('TEST', 'PROD') THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request, 'environment debe ser TEST o PROD');
    END IF;
    l_ct_hex    := json_value(p_body, '$.secret_ciphertext' RETURNING VARCHAR2(32767));
    l_nonce_hex := json_value(p_body, '$.secret_nonce' RETURNING VARCHAR2(64));
    IF l_ct_hex IS NULL OR l_nonce_hex IS NULL THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request, 'secret_ciphertext y secret_nonce requeridos');
    END IF;
    l_ct    := TO_BLOB(HEXTORAW(l_ct_hex));
    l_nonce := HEXTORAW(l_nonce_hex);

    pkg_esign_session.set_client(p_client_id);

    MERGE INTO client_webhook_endpoint t
    USING (SELECT p_client_id AS cid, l_env AS env FROM dual) s
    ON (t.client_id = s.cid AND t.environment = s.env)
    WHEN MATCHED THEN UPDATE SET
      secret_ciphertext = l_ct,
      secret_nonce = l_nonce,
      key_version = l_ver,
      updated_at = SYSTIMESTAMP
    WHEN NOT MATCHED THEN INSERT
      (client_id, environment, secret_ciphertext, secret_nonce, key_version, is_active)
    VALUES
      (p_client_id, l_env, l_ct, l_nonce, l_ver, 0);

    p_out := pkg_esign_util.fn_ok(fn_config_json(p_client_id, l_env));
  END pr_store_secret;

  PROCEDURE pr_get_secret_json(
    p_client_id   IN NUMBER,
    p_environment IN VARCHAR2,
    p_out         OUT CLOB
  ) IS
    l_env  VARCHAR2(4) := UPPER(p_environment);
    l_data CLOB;
  BEGIN
    IF l_env NOT IN ('TEST', 'PROD') THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request, 'environment debe ser TEST o PROD');
    END IF;
    pkg_esign_session.set_client(p_client_id);

    SELECT JSON_OBJECT(
             'secret_ciphertext' VALUE pkg_esign_util.fn_blob_hex(w.secret_ciphertext),
             'secret_nonce' VALUE RAWTOHEX(w.secret_nonce),
             'key_version' VALUE w.key_version,
             'url' VALUE w.url,
             'is_active' VALUE w.is_active
             RETURNING CLOB)
      INTO l_data
      FROM client_webhook_endpoint w
     WHERE w.client_id = p_client_id
       AND w.environment = l_env
       AND w.is_active = 1
       AND w.url IS NOT NULL
       AND DBMS_LOB.GETLENGTH(w.secret_ciphertext) > 0;

    p_out := pkg_esign_util.fn_ok(l_data);
  EXCEPTION
    WHEN NO_DATA_FOUND THEN
      raise_application_error(pkg_esign_http.c_ora_not_found, 'webhook no configurado o inactivo');
  END pr_get_secret_json;

END pkg_esign_webhook_api;
/
