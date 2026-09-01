-- Completar secretos de la Web Credential OCI_BUCKET_CRED en workspace ESIGN.
-- Copiar User OCID / Tenancy OCID / Fingerprint desde AOXDEV > Shared Components >
-- Web Credentials > OCI_BUCKET_CRED (la private key NO se puede leer; usar el .pem original).
-- Ejecutar conectado como ESIGN (o desde SQL Workshop del workspace ESIGN).
SET DEFINE OFF;
BEGIN
  apex_util.set_workspace('ESIGN');
  apex_session.create_session(p_app_id => 104, p_page_id => 1, p_username => 'ESIGN');

  apex_credential.set_persistent_credentials(
    p_credential_static_id => 'OCI_BUCKET_CRED',
    p_client_id            => 'ocid1.user.oc1..XXXXXXXX',      -- OCI User OCID
    p_client_secret        => '-----BEGIN PRIVATE KEY-----
...
-----END PRIVATE KEY-----',                                  -- PEM (sin password)
    p_namespace            => 'ocid1.tenancy.oc1..XXXXXXXX',   -- Tenancy OCID
    p_fingerprint          => 'aa:bb:cc:...'                  -- Public key fingerprint
  );

  apex_session.delete_session;
  COMMIT;
END;
/
