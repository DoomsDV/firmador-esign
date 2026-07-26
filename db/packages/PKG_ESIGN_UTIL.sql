-- PKG_ESIGN_UTIL: utilidades transversales.
--  * Hash de passwords con salt por usuario (SHA-512 iterado sobre DBMS_CRYPTO).
--  * Hash de API keys (SHA-256 sobre la key de alta entropia; no requiere salt).
--  * Helpers de JWT (decode/validate via apex_jwt) y lectura de claims.
--  * Validacion del X-Service-Token (Go<->ORDS).
--  * Generacion de tokens/salt aleatorios y lectura de app_parameter.
CREATE OR REPLACE PACKAGE pkg_esign_util AS

  -- Iteraciones del hash de password (stretching). DBMS_CRYPTO.hash es una llamada nativa,
  -- asi que el loop no genera round-trips SQL.
  c_pwd_iterations CONSTANT PLS_INTEGER := 1000;

  FUNCTION fn_get_parameter(p_key IN VARCHAR2) RETURN VARCHAR2;

  -- Aleatoriedad
  FUNCTION fn_random_hex(p_bytes IN PLS_INTEGER) RETURN VARCHAR2;
  FUNCTION fn_new_salt RETURN VARCHAR2;

  -- Passwords
  FUNCTION fn_hash_password(p_password IN VARCHAR2, p_salt IN VARCHAR2) RETURN VARCHAR2;
  FUNCTION fn_verify_password(p_password IN VARCHAR2, p_salt IN VARCHAR2, p_hash IN VARCHAR2) RETURN BOOLEAN;

  -- API keys
  FUNCTION fn_hash_api_key(p_key IN VARCHAR2) RETURN VARCHAR2;

  -- JWT
  FUNCTION fn_validate_jwt(p_authorization IN VARCHAR2) RETURN VARCHAR2; -- devuelve el payload JSON o lanza error
  FUNCTION fn_get_client_id_from_jwt(p_authorization IN VARCHAR2) RETURN NUMBER;
  FUNCTION fn_get_user_id_from_jwt(p_authorization IN VARCHAR2) RETURN NUMBER;
  FUNCTION fn_get_role_from_jwt(p_authorization IN VARCHAR2) RETURN VARCHAR2;

  -- Servicio interno
  FUNCTION fn_service_token_ok(p_token IN VARCHAR2) RETURN BOOLEAN;

  -- Envelopes de respuesta estandar. Se construyen con SELECT INTO (JSON_OBJECT es SQL) y se
  -- invocan desde PL/SQL con p_out := fn_ok(l_data). p_data debe ser JSON valido (o NULL).
  FUNCTION fn_ok(p_data IN CLOB DEFAULT NULL) RETURN CLOB;
  FUNCTION fn_ok_meta(p_data IN CLOB, p_meta IN CLOB) RETURN CLOB;
  FUNCTION fn_err(p_code IN VARCHAR2, p_message IN VARCHAR2) RETURN CLOB;

  -- fn_blob_hex convierte un BLOB a su representacion hex (RAWTOHEX no acepta BLOB).
  FUNCTION fn_blob_hex(p_blob IN BLOB) RETURN CLOB;

  -- fn_hex_to_blob convierte un hex (CLOB, puede superar 32767) a BLOB por chunks.
  -- Tiene efectos (DBMS_LOB): precomputar en variable, no usar dentro de SQL.
  FUNCTION fn_hex_to_blob(p_hex IN CLOB) RETURN BLOB;

END pkg_esign_util;
/

CREATE OR REPLACE PACKAGE BODY pkg_esign_util AS

  FUNCTION fn_get_parameter(p_key IN VARCHAR2) RETURN VARCHAR2 IS
    l_val app_parameter.param_value%TYPE;
  BEGIN
    SELECT param_value INTO l_val FROM app_parameter WHERE param_key = p_key;
    RETURN l_val;
  EXCEPTION
    WHEN NO_DATA_FOUND THEN RETURN NULL;
  END fn_get_parameter;

  -- fn_random_hex genera p_bytes aleatorios criptograficos en hex (DBMS_CRYPTO.randombytes).
  FUNCTION fn_random_hex(p_bytes IN PLS_INTEGER) RETURN VARCHAR2 IS
  BEGIN
    RETURN LOWER(RAWTOHEX(dbms_crypto.randombytes(p_bytes)));
  END fn_random_hex;

  FUNCTION fn_new_salt RETURN VARCHAR2 IS
  BEGIN
    RETURN fn_random_hex(16); -- 32 hex chars
  END fn_new_salt;

  -- SHA-512 salteado e iterado: h0 = SHA512(salt||password); hi = SHA512(h(i-1)||salt).
  FUNCTION fn_hash_password(p_password IN VARCHAR2, p_salt IN VARCHAR2) RETURN VARCHAR2 IS
    l_hash RAW(64);
    l_in   RAW(4000);
  BEGIN
    l_in   := utl_raw.cast_to_raw(p_salt || p_password);
    l_hash := dbms_crypto.hash(l_in, dbms_crypto.hash_sh512);
    FOR i IN 1 .. c_pwd_iterations LOOP
      l_hash := dbms_crypto.hash(
        utl_raw.concat(l_hash, utl_raw.cast_to_raw(p_salt)),
        dbms_crypto.hash_sh512);
    END LOOP;
    RETURN LOWER(RAWTOHEX(l_hash));
  END fn_hash_password;

  FUNCTION fn_verify_password(p_password IN VARCHAR2, p_salt IN VARCHAR2, p_hash IN VARCHAR2) RETURN BOOLEAN IS
  BEGIN
    RETURN LOWER(p_hash) = fn_hash_password(p_password, p_salt);
  END fn_verify_password;

  -- SHA-256 sobre la key de alta entropia (no requiere salt).
  FUNCTION fn_hash_api_key(p_key IN VARCHAR2) RETURN VARCHAR2 IS
  BEGIN
    RETURN LOWER(RAWTOHEX(dbms_crypto.hash(utl_raw.cast_to_raw(p_key), dbms_crypto.hash_sh256)));
  END fn_hash_api_key;

  FUNCTION fn_strip_bearer(p_authorization IN VARCHAR2) RETURN VARCHAR2 IS
    l_auth VARCHAR2(4000) := TRIM(p_authorization);
  BEGIN
    IF l_auth IS NULL THEN
      RETURN NULL;
    END IF;
    IF REGEXP_LIKE(l_auth, '^Bearer ', 'i') THEN
      RETURN TRIM(SUBSTR(l_auth, 8));
    END IF;
    RETURN l_auth;
  END fn_strip_bearer;

  FUNCTION fn_validate_jwt(p_authorization IN VARCHAR2) RETURN VARCHAR2 IS
    l_token VARCHAR2(4000) := fn_strip_bearer(p_authorization);
    l_jwt   apex_jwt.t_token;
  BEGIN
    IF l_token IS NULL THEN
      raise_application_error(-20401, 'token ausente');
    END IF;
    l_jwt := apex_jwt.decode(
      p_value         => l_token,
      p_signature_key => utl_raw.cast_to_raw(fn_get_parameter('JWT_TOKEN'))
    );
    apex_jwt.validate(
      p_token => l_jwt,
      p_iss   => fn_get_parameter('JWT_ISSUER'),
      p_aud   => fn_get_parameter('JWT_AUDIENCE')
    );
    RETURN l_jwt.payload;
  END fn_validate_jwt;

  FUNCTION fn_claim(p_payload IN VARCHAR2, p_claim IN VARCHAR2) RETURN VARCHAR2 IS
  BEGIN
    RETURN json_value(p_payload, '$.' || p_claim);
  END fn_claim;

  FUNCTION fn_get_client_id_from_jwt(p_authorization IN VARCHAR2) RETURN NUMBER IS
  BEGIN
    RETURN TO_NUMBER(fn_claim(fn_validate_jwt(p_authorization), 'client_id'));
  END fn_get_client_id_from_jwt;

  FUNCTION fn_get_user_id_from_jwt(p_authorization IN VARCHAR2) RETURN NUMBER IS
  BEGIN
    RETURN TO_NUMBER(fn_claim(fn_validate_jwt(p_authorization), 'user_id'));
  END fn_get_user_id_from_jwt;

  FUNCTION fn_get_role_from_jwt(p_authorization IN VARCHAR2) RETURN VARCHAR2 IS
  BEGIN
    RETURN fn_claim(fn_validate_jwt(p_authorization), 'role');
  END fn_get_role_from_jwt;

  FUNCTION fn_service_token_ok(p_token IN VARCHAR2) RETURN BOOLEAN IS
    l_expected VARCHAR2(4000) := fn_get_parameter('SERVICE_TOKEN');
  BEGIN
    RETURN l_expected IS NOT NULL AND p_token IS NOT NULL AND p_token = l_expected;
  END fn_service_token_ok;

  FUNCTION fn_ok(p_data IN CLOB DEFAULT NULL) RETURN CLOB IS
    l CLOB;
  BEGIN
    IF p_data IS NULL THEN
      SELECT JSON_OBJECT('success' VALUE 'true' FORMAT JSON RETURNING CLOB) INTO l FROM dual;
    ELSE
      SELECT JSON_OBJECT('success' VALUE 'true' FORMAT JSON, 'data' VALUE p_data FORMAT JSON RETURNING CLOB)
        INTO l FROM dual;
    END IF;
    RETURN l;
  END fn_ok;

  FUNCTION fn_ok_meta(p_data IN CLOB, p_meta IN CLOB) RETURN CLOB IS
    l CLOB;
  BEGIN
    SELECT JSON_OBJECT('success' VALUE 'true' FORMAT JSON,
                       'data' VALUE p_data FORMAT JSON,
                       'meta' VALUE p_meta FORMAT JSON RETURNING CLOB)
      INTO l FROM dual;
    RETURN l;
  END fn_ok_meta;

  FUNCTION fn_err(p_code IN VARCHAR2, p_message IN VARCHAR2) RETURN CLOB IS
    l CLOB;
  BEGIN
    SELECT JSON_OBJECT('success' VALUE 'false' FORMAT JSON,
                       'error' VALUE JSON_OBJECT('code' VALUE p_code, 'message' VALUE p_message) RETURNING CLOB)
      INTO l FROM dual;
    RETURN l;
  END fn_err;

  FUNCTION fn_blob_hex(p_blob IN BLOB) RETURN CLOB IS
    l_hex    CLOB;
    l_offset INTEGER := 1;
    l_amt    INTEGER;
    l_raw    RAW(8000);
    l_len    INTEGER;
  BEGIN
    IF p_blob IS NULL THEN
      RETURN NULL;
    END IF;
    l_len := dbms_lob.getlength(p_blob);
    dbms_lob.createtemporary(l_hex, TRUE);
    WHILE l_offset <= l_len LOOP
      l_amt := 8000;
      dbms_lob.read(p_blob, l_amt, l_offset, l_raw);
      l_hex := l_hex || LOWER(RAWTOHEX(l_raw));
      l_offset := l_offset + l_amt;
    END LOOP;
    RETURN l_hex;
  END fn_blob_hex;

  FUNCTION fn_hex_to_blob(p_hex IN CLOB) RETURN BLOB IS
    l_blob  BLOB;
    l_len   INTEGER;
    l_off   INTEGER := 1;
    l_chunk INTEGER := 8000; -- par: cada byte son 2 hex; 8000 hex = 4000 bytes
    l_sub   VARCHAR2(32767);
  BEGIN
    IF p_hex IS NULL THEN
      RETURN NULL;
    END IF;
    l_len := dbms_lob.getlength(p_hex);
    IF l_len = 0 THEN
      RETURN NULL;
    END IF;
    dbms_lob.createtemporary(l_blob, TRUE);
    WHILE l_off <= l_len LOOP
      l_sub := dbms_lob.substr(p_hex, l_chunk, l_off);
      dbms_lob.append(l_blob, TO_BLOB(HEXTORAW(l_sub)));
      l_off := l_off + l_chunk;
    END LOOP;
    RETURN l_blob;
  END fn_hex_to_blob;

END pkg_esign_util;
/
