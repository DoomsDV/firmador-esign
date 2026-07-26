-- PKG_ESIGN_CERT_API: gestion del certificado .p12 (siempre cifrado por Go, AES-256-GCM).
-- pr_put_certificate recibe ciphertext+nonce+metadata y los persiste opacos en client.
-- pr_get_certificate es SOLO interno (Go): devuelve los blobs cifrados. El BLOB nunca se
-- expone al panel; el panel solo ve metadata (subject/vigencia/estado) via pr_get_meta.
CREATE OR REPLACE PACKAGE pkg_esign_cert_api AS

  PROCEDURE pr_put_certificate(p_client_id IN NUMBER, p_role IN VARCHAR2, p_body IN CLOB, p_out OUT CLOB);
  PROCEDURE pr_get_meta(p_client_id IN NUMBER, p_out OUT CLOB);

  -- Interno (Go): blobs cifrados + password cifrado + key_version.
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
    -- puede usarse dentro del SQL del UPDATE).
    l_p12 BLOB;
    l_pwd BLOB;
  BEGIN
    IF p_role <> 'owner' THEN
      raise_application_error(-20403, 'solo el owner puede subir el certificado');
    END IF;
    pkg_esign_session.set_client(p_client_id);

    l_p12 := pkg_esign_util.fn_hex_to_blob(json_value(p_body, '$.p12_ciphertext' RETURNING CLOB));
    l_pwd := pkg_esign_util.fn_hex_to_blob(json_value(p_body, '$.pwd_ciphertext' RETURNING CLOB));

    UPDATE client SET
      cert_subject_dn     = json_value(p_body, '$.subject_dn' RETURNING VARCHAR2(400)),
      cert_not_after      = TO_TIMESTAMP_TZ(json_value(p_body, '$.not_after'), 'YYYY-MM-DD"T"HH24:MI:SSTZH:TZM'),
      cert_p12_ciphertext = l_p12,
      cert_p12_nonce      = HEXTORAW(json_value(p_body, '$.p12_nonce')),
      cert_pwd_ciphertext = l_pwd,
      cert_pwd_nonce      = HEXTORAW(json_value(p_body, '$.pwd_nonce')),
      cert_key_version    = NVL(json_value(p_body, '$.key_version'), 1),
      cert_status         = 'ACTIVE'
    WHERE id_client = p_client_id;

    p_out := pkg_esign_util.fn_ok;
  END pr_put_certificate;

  PROCEDURE pr_get_meta(p_client_id IN NUMBER, p_out OUT CLOB) IS
    l_data CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);
    SELECT JSON_OBJECT(
             'subject_dn' VALUE cert_subject_dn,
             'not_after'  VALUE TO_CHAR(cert_not_after, 'YYYY-MM-DD"T"HH24:MI:SSTZH:TZM'),
             'status'     VALUE NVL(cert_status, 'NONE'),
             'key_version' VALUE cert_key_version
             RETURNING CLOB)
      INTO l_data FROM client WHERE id_client = p_client_id;
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
    SELECT cert_p12_ciphertext, cert_p12_nonce, cert_pwd_ciphertext, cert_pwd_nonce, cert_key_version
      INTO p_p12_ciphertext, p_p12_nonce, p_pwd_ciphertext, p_pwd_nonce, p_key_version
      FROM client WHERE id_client = p_client_id;
    IF p_p12_ciphertext IS NULL THEN
      raise_application_error(-20404, 'el cliente no tiene certificado cargado');
    END IF;
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
