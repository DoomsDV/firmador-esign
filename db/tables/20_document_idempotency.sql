-- document_idempotency: reclamo atómico previo a construir, firmar y enviar un DE.
-- Un registro IN_FLIGHT es una emisión en curso; COMPLETED referencia el resultado
-- persistido en document. La PK garantiza una sola emisión por tenant/ambiente/clave.
CREATE TABLE document_idempotency (
  client_id       NUMBER NOT NULL,
  environment     VARCHAR2(4) NOT NULL,
  idempotency_key VARCHAR2(128) NOT NULL,
  status          VARCHAR2(12) NOT NULL,
  cdc             VARCHAR2(44),
  phase           VARCHAR2(30) DEFAULT 'CLAIMED' NOT NULL,
  request_sha256  VARCHAR2(64),
  xml_sha256      VARCHAR2(64),
  lease_until     TIMESTAMP(6) WITH TIME ZONE,
  recovery_note   VARCHAR2(500),
  created_at      TIMESTAMP(6) WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP NOT NULL,
  updated_at      TIMESTAMP(6) WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP NOT NULL,
  CONSTRAINT pk_document_idempotency PRIMARY KEY (client_id, environment, idempotency_key),
  CONSTRAINT fk_document_idem_client FOREIGN KEY (client_id)
    REFERENCES client (id_client) ON DELETE CASCADE,
  CONSTRAINT ck_document_idem_env CHECK (environment IN ('TEST', 'PROD')),
  CONSTRAINT ck_document_idem_status CHECK (status IN ('IN_FLIGHT', 'COMPLETED')),
  CONSTRAINT ck_document_idem_phase CHECK (
    phase IN ('CLAIMED', 'FIRMADO', 'RECOVERY_REQUIRED', 'COMPLETED')
  )
);

COMMENT ON TABLE document_idempotency IS
  'Reclamo atómico de Idempotency-Key; conserva la recuperación de una emisión ambigua.';
COMMENT ON COLUMN document_idempotency.request_sha256 IS
  'SHA-256 del payload normalizado; impide reutilizar una key con otro documento.';
COMMENT ON COLUMN document_idempotency.xml_sha256 IS
  'SHA-256 del XML firmado asociado a la operación lógica.';
COMMENT ON COLUMN document_idempotency.lease_until IS
  'Lease del claim inicial; al vencer con CDC requiere recuperación, nunca reemisión nueva.';
