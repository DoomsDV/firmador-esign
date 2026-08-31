-- Reclamo atómico de Idempotency-Key antes de la emisión SIFEN.
PROMPT === document_idempotency ===

DECLARE
  l_count NUMBER;
BEGIN
  SELECT COUNT(*) INTO l_count
    FROM user_tables
   WHERE table_name = 'DOCUMENT_IDEMPOTENCY';

  IF l_count = 0 THEN
    EXECUTE IMMEDIATE q'[
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
      )
    ]';
  END IF;
END;
/

-- Las claves registradas antes de esta migración ya representan emisiones completas.
MERGE /*+ no_parallel */ INTO document_idempotency i
USING (
  SELECT client_id, environment, idempotency_key, cdc
    FROM document
   WHERE idempotency_key IS NOT NULL
) d
ON (i.client_id = d.client_id
    AND i.environment = d.environment
    AND i.idempotency_key = d.idempotency_key)
WHEN NOT MATCHED THEN INSERT (
  client_id, environment, idempotency_key, status, cdc, created_at, updated_at
) VALUES (
  d.client_id, d.environment, d.idempotency_key, 'COMPLETED', d.cdc, SYSTIMESTAMP, SYSTIMESTAMP
);

BEGIN
  dbms_rls.drop_policy(
    object_schema => sys_context('userenv', 'current_schema'),
    object_name   => 'DOCUMENT_IDEMPOTENCY',
    policy_name   => 'VPD_DOCUMENT_IDEMPOTENCY'
  );
EXCEPTION
  WHEN OTHERS THEN
    NULL;
END;
/

BEGIN
  dbms_rls.add_policy(
    object_schema   => sys_context('userenv', 'current_schema'),
    object_name     => 'DOCUMENT_IDEMPOTENCY',
    policy_name     => 'VPD_DOCUMENT_IDEMPOTENCY',
    function_schema => sys_context('userenv', 'current_schema'),
    policy_function => 'FN_TENANT_POLICY',
    statement_types => 'SELECT,INSERT,UPDATE,DELETE',
    update_check    => TRUE
  );
END;
/

COMMENT ON TABLE document_idempotency IS
  'Reclamo atómico de Idempotency-Key antes de la emisión SIFEN.';

COMMIT;
PROMPT === OK: document_idempotency ===
