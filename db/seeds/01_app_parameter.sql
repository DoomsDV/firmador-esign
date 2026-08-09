-- Seeds de app_parameter. Los secretos (JWT_TOKEN, SERVICE_TOKEN) se generan aleatorios si
-- no existen; reemplazar por valores gestionados en produccion. Idempotente (MERGE por clave).
DECLARE
  PROCEDURE upsert(p_key VARCHAR2, p_val VARCHAR2, p_desc VARCHAR2) IS
  BEGIN
    MERGE INTO app_parameter t
    USING (SELECT p_key AS k FROM dual) s
    ON (t.param_key = s.k)
    WHEN NOT MATCHED THEN INSERT (param_key, param_value, description)
    VALUES (p_key, p_val, p_desc);
  END;
BEGIN
  upsert('JWT_ISSUER',           'esign-api',  'Issuer de los JWT del panel');
  upsert('JWT_AUDIENCE',         'esign-app',  'Audience de los JWT del panel');
  upsert('JWT_ACCESS_EXP_SEC',   '3600',       'Vigencia del access token (segundos)');
  upsert('JWT_REFRESH_EXP_DAYS', '30',         'Vigencia del refresh token (dias)');
  -- Secreto de firma HMAC de los JWT (64 hex). Rotar en produccion.
  upsert('JWT_TOKEN',     LOWER(RAWTOHEX(dbms_crypto.randombytes(32))), 'Clave HMAC de firma de los JWT');
  -- Token compartido Go<->ORDS para el modulo interno.
  upsert('SERVICE_TOKEN', LOWER(RAWTOHEX(dbms_crypto.randombytes(24))), 'X-Service-Token (Go<->ORDS interno)');
  -- OCI Object Storage (bucket-etick-dev). Credencial APEX: OCI_BUCKET_CRED.
  upsert(
    'OCI_BUCKET_BASE_URL',
    'https://objectstorage.sa-saopaulo-1.oraclecloud.com/n/gr7djv0kcgrr/b/bucket-etick-dev/o',
    'Base URL Object Storage (sin slash final obligatorio; el paquete lo normaliza)'
  );
  upsert('OCI_CREDENTIAL_NAME', 'OCI_BUCKET_CRED', 'Static ID de la Web Credential APEX (tipo OCI)');
  COMMIT;
END;
/
