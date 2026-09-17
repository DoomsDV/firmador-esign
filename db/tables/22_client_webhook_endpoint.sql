-- client_webhook_endpoint: URL de entrega por cliente y ambiente (TEST|PROD).
-- El secret HMAC se guarda cifrado (AES-256-GCM en Go); Oracle solo ve ciphertext+nonce.
CREATE TABLE client_webhook_endpoint (
  client_id         NUMBER NOT NULL,
  environment       VARCHAR2(4) NOT NULL,
  url               VARCHAR2(500),
  secret_ciphertext BLOB,
  secret_nonce      RAW(16),
  key_version       NUMBER DEFAULT 1 NOT NULL,
  is_active         NUMBER(1) DEFAULT 0 NOT NULL,
  updated_at        TIMESTAMP(6) WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP NOT NULL,
  CONSTRAINT pk_client_webhook PRIMARY KEY (client_id, environment),
  CONSTRAINT fk_webhook_client FOREIGN KEY (client_id) REFERENCES client (id_client) ON DELETE CASCADE,
  CONSTRAINT ck_webhook_env CHECK (environment IN ('TEST', 'PROD')),
  CONSTRAINT ck_webhook_active CHECK (is_active IN (0, 1))
);

COMMENT ON TABLE client_webhook_endpoint IS 'Webhook de entrega por cliente/ambiente. Secret cifrado en Go; URL HTTPS configurada en el panel.';
COMMENT ON COLUMN client_webhook_endpoint.secret_ciphertext IS 'Secret HMAC cifrado AES-256-GCM (clave maestra en Go).';
COMMENT ON COLUMN client_webhook_endpoint.is_active IS '1=el firmador encola entregas invoice.ready hacia url.';
