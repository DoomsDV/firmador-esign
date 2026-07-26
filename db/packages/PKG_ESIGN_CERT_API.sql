-- PKG_ESIGN_CERT_API: gestion del certificado .p12 (siempre cifrado por Go, AES-256-GCM),
-- respaldada por la tabla hija client_certificate (historial; a lo sumo uno ACTIVE).
-- pr_put_certificate recibe ciphertext+nonce+metadata, desactiva el ACTIVE previo e inserta
-- el nuevo. pr_get_certificate es SOLO interno (Go): devuelve los blobs cifrados del ACTIVE.
-- El BLOB nunca se expone al panel; el panel solo ve metadata (subject/vigencia/estado).
CREATE OR REPLACE PACKAGE pkg_esign_cert_api AS

  PROCEDURE pr_put_certificate(p_client_id IN NUMBER, p_role IN VARCHAR2, p_body IN CLOB, p_out OUT CLOB);
  PROCEDURE pr_get_meta(p_client_id IN NUMBER, p_out OUT CLOB);

  -- Interno (Go): blobs cifrados + password cifrado + key_version del certificado ACTIVE.
  PROCEDURE pr_get_certificate(
    p_client_id      IN  NUMBER,
    p_p12_ciphertext OUT BLOB,
    p_p12_nonce      OUT RAW,
    p_pwd_ciphertext OUT BLOB,
    p_pwd_nonce      OUT RAW,
    p_key_version    OUT NUMBER
  );

  -- Interno (Go) en formato JSON con blobs en hex (mas comodo para ORDS/HTTP).
  PROCEDURE pr_get_certificate_json(p_client_id IN NUMBER, p_out OUT CLOB);

END pkg_esign_cert_api;
/

CREATE OR REPLACE PACKAGE BODY pkg_esign_cert_api AS

  PROCEDURE pr_put_certificate(p_client_id IN NUMBER, p_role IN VARCHAR2, p_body IN CLOB, p_out OUT CLOB) IS
    -- El hex del .p12 supera VARCHAR2(4000): se extrae con RETURNING CLOB y se
    -- convierte a BLOB por chunks en variables (fn_hex_to_blob tiene efectos y no
    -- puede usarse dentro del SQL del INSERT).
    l_p12 BLOB;
    l_pwd BLOB;
  BEGIN
    IF p_role <> 'owner' THEN
      raise_application_error(pkg_esign_http.c_ora_forbidden, 'solo el owner puede subir el certificado');
    END IF;
    pkg_esign_session.set_client(p_client_id);

    l_p12 := pkg_esign_util.fn_hex_to_blob(json_value(p_body, '$.p12_ciphertext' RETURNING CLOB));
    l_pwd := pkg_esign_util.fn_hex_to_blob(json_value(p_body, '$.pwd_ciphertext' RETURNING CLOB));

    -- El certificado ACTIVE previo pasa a INACTIVE (historial); entra el nuevo como ACTIVE.
    UPDATE /*+ no_parallel */ client_certificate
       SET status = 'INACTIVE'
     WHERE client_id = p_client_id AND status = 'ACTIVE';

    INSERT /*+ no_parallel */ INTO client_certificate (
      client_id, subject_dn, not_after,
      p12_ciphertext, p12_nonce, pwd_ciphertext, pwd_nonce, key_version, status)
    VALUES (
      p_client_id,
      json_value(p_body, '$.subject_dn' RETURNING VARCHAR2(400)),
      TO_TIMESTAMP_TZ(json_value(p_body, '$.not_after'), 'YYYY-MM-DD"T"HH24:MI:SSTZH:TZM'),
      l_p12, HEXTORAW(json_value(p_body, '$.p12_nonce')),
      l_pwd, HEXTORAW(json_value(p_body, '$.pwd_nonce')),
      NVL(json_value(p_body, '$.key_version'), 1), 'ACTIVE');

    p_out := pkg_esign_util.fn_ok;
  END pr_put_certificate;

  PROCEDURE pr_get_meta(p_client_id IN NUMBER, p_out OUT CLOB) IS
    l_data CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);
    BEGIN
      SELECT JSON_OBJECT(
               'subject_dn'  VALUE subject_dn,
               'not_after'   VALUE TO_CHAR(not_after, 'YYYY-MM-DD"T"HH24:MI:SSTZH:TZM'),
               'status'      VALUE status,
               'key_version' VALUE key_version
               RETURNING CLOB)
        INTO l_data
        FROM client_certificate
       WHERE client_id = p_client_id AND status = 'ACTIVE'
       ORDER BY created_at DESC
       FETCH FIRST 1 ROWS ONLY;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN
        SELECT JSON_OBJECT('status' VALUE 'NONE' RETURNING CLOB) INTO l_data FROM dual;
    END;
    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_get_meta;

  PROCEDURE pr_get_certificate(
    p_client_id      IN  NUMBER,
    p_p12_ciphertext OUT BLOB,
    p_p12_nonce      OUT RAW,
    p_pwd_ciphertext OUT BLOB,
    p_pwd_nonce      OUT RAW,
    p_key_version    OUT NUMBER
  ) IS
  BEGIN
    pkg_esign_session.set_client(p_client_id);
    BEGIN
      SELECT p12_ciphertext, p12_nonce, pwd_ciphertext, pwd_nonce, key_version
        INTO p_p12_ciphertext, p_p12_nonce, p_pwd_ciphertext, p_pwd_nonce, p_key_version
        FROM client_certificate
       WHERE client_id = p_client_id AND status = 'ACTIVE'
       ORDER BY created_at DESC
       FETCH FIRST 1 ROWS ONLY;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN
        raise_application_error(pkg_esign_http.c_ora_not_found, 'el cliente no tiene certificado cargado');
    END;
  END pr_get_certificate;

  PROCEDURE pr_get_certificate_json(p_client_id IN NUMBER, p_out OUT CLOB) IS
    l_p12  BLOB; l_p12n RAW(16); l_pwd BLOB; l_pwdn RAW(16); l_kv NUMBER;
    l_data CLOB;
    l_p12hex CLOB;
    l_pwdhex CLOB;
  BEGIN
    pr_get_certificate(p_client_id, l_p12, l_p12n, l_pwd, l_pwdn, l_kv);
    l_p12hex := pkg_esign_util.fn_blob_hex(l_p12);
    l_pwdhex := pkg_esign_util.fn_blob_hex(l_pwd);
    SELECT JSON_OBJECT(
        'p12_ciphertext' VALUE l_p12hex,
        'p12_nonce'      VALUE LOWER(RAWTOHEX(l_p12n)),
        'pwd_ciphertext' VALUE l_pwdhex,
        'pwd_nonce'      VALUE LOWER(RAWTOHEX(l_pwdn)),
        'key_version'    VALUE l_kv RETURNING CLOB)
      INTO l_data FROM dual;
    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_get_certificate_json;

END pkg_esign_cert_api;
/
