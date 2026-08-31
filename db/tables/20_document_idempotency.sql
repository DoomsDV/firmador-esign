-- document_idempotency: reclamo atómico previo a construir, firmar y enviar un DE.
-- Un registro IN_FLIGHT es una emisión en curso; COMPLETED referencia el resultado
-- persistido en document. La PK garantiza una sola emisión por tenant/ambiente/clave.
CREATE TABLE document_idempotency (
  client_id       NUMBER NOT NULL,
  environment     VARCHAR2(4) NOT NULL,
  idempotency_key VARCHAR2(128) NOT NULL,
  status          VARCHAR2(12) NOT NULL,
  cdc             VARCHAR2(44),
  created_at      TIMESTAMP(6) WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP NOT NULL,
  updated_at      TIMESTAMP(6) WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP NOT NULL,
  CONSTRAINT pk_document_idempotency PRIMARY KEY (client_id, environment, idempotency_key),
  CONSTRAINT fk_document_idem_client FOREIGN KEY (client_id)
    REFERENCES client (id_client) ON DELETE CASCADE,
  CONSTRAINT ck_document_idem_env CHECK (environment IN ('TEST', 'PROD')),
  CONSTRAINT ck_document_idem_status CHECK (status IN ('IN_FLIGHT', 'COMPLETED'))
);

COMMENT ON TABLE document_idempotency IS
  'Reclamo atómico de Idempotency-Key antes de la emisión SIFEN.';
