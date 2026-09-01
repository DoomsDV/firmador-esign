-- Resiliencia de emisión: idempotencia recuperable, reintentos terminales y
-- recuperación durable de KuDE. Seguro para ejecutar más de una vez.
PROMPT === resiliencia fiscal ===

DECLARE
  l_schema VARCHAR2(128) := SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA');

  PROCEDURE add_column(p_table IN VARCHAR2, p_ddl IN VARCHAR2) IS
  BEGIN
    EXECUTE IMMEDIATE p_ddl;
  EXCEPTION
    WHEN OTHERS THEN
      IF SQLCODE = -1430 THEN
        NULL;
      ELSE
        RAISE;
      END IF;
  END add_column;

  PROCEDURE set_policy(p_table IN VARCHAR2, p_policy IN VARCHAR2, p_enabled IN BOOLEAN) IS
  BEGIN
    DBMS_RLS.ENABLE_POLICY(l_schema, p_table, p_policy, p_enabled);
  EXCEPTION
    WHEN OTHERS THEN
      NULL;
  END set_policy;
BEGIN
  set_policy('DOCUMENT_IDEMPOTENCY', 'VPD_DOCUMENT_IDEMPOTENCY', FALSE);
  set_policy('DOCUMENT', 'VPD_DOCUMENT', FALSE);

  BEGIN
    add_column(
      'DOCUMENT_IDEMPOTENCY',
      'ALTER TABLE document_idempotency ADD (phase VARCHAR2(30) DEFAULT ''CLAIMED'')'
    );
    add_column(
      'DOCUMENT_IDEMPOTENCY',
      'ALTER TABLE document_idempotency ADD (request_sha256 VARCHAR2(64))'
    );
    add_column(
      'DOCUMENT_IDEMPOTENCY',
      'ALTER TABLE document_idempotency ADD (xml_sha256 VARCHAR2(64))'
    );
    add_column(
      'DOCUMENT_IDEMPOTENCY',
      'ALTER TABLE document_idempotency ADD (lease_until TIMESTAMP(6) WITH TIME ZONE)'
    );
    add_column(
      'DOCUMENT_IDEMPOTENCY',
      'ALTER TABLE document_idempotency ADD (recovery_note VARCHAR2(500))'
    );

    add_column(
      'DOCUMENT',
      'ALTER TABLE document ADD (recovery_required NUMBER(1) DEFAULT 0 NOT NULL)'
    );
    add_column(
      'DOCUMENT',
      'ALTER TABLE document ADD (recovery_reason VARCHAR2(500))'
    );
    add_column(
      'DOCUMENT',
      'ALTER TABLE document ADD (recovery_marked_at TIMESTAMP(6) WITH TIME ZONE)'
    );
  EXCEPTION
    WHEN OTHERS THEN
      set_policy('DOCUMENT', 'VPD_DOCUMENT', TRUE);
      set_policy('DOCUMENT_IDEMPOTENCY', 'VPD_DOCUMENT_IDEMPOTENCY', TRUE);
      RAISE;
  END;

  set_policy('DOCUMENT', 'VPD_DOCUMENT', TRUE);
  set_policy('DOCUMENT_IDEMPOTENCY', 'VPD_DOCUMENT_IDEMPOTENCY', TRUE);
END;
/

UPDATE /*+ no_parallel */ document_idempotency
   SET phase = CASE
                 WHEN status = 'COMPLETED' THEN 'COMPLETED'
                 WHEN cdc IS NOT NULL THEN 'RECOVERY_REQUIRED'
                 ELSE 'CLAIMED'
               END,
       lease_until = CASE
                       WHEN status = 'IN_FLIGHT' THEN SYSTIMESTAMP - NUMTODSINTERVAL(1, 'SECOND')
                       ELSE NULL
                     END,
       updated_at = SYSTIMESTAMP
 WHERE phase IS NULL;

BEGIN
  EXECUTE IMMEDIATE
    'ALTER TABLE document_idempotency ADD CONSTRAINT ck_document_idem_phase ' ||
    'CHECK (phase IN (''CLAIMED'', ''FIRMADO'', ''RECOVERY_REQUIRED'', ''COMPLETED''))';
EXCEPTION
  WHEN OTHERS THEN
    IF SQLCODE NOT IN (-2260, -2264) THEN
      RAISE;
    END IF;
END;
/

BEGIN
  EXECUTE IMMEDIATE
    'ALTER TABLE document ADD CONSTRAINT ck_document_recovery_required ' ||
    'CHECK (recovery_required IN (0, 1))';
EXCEPTION
  WHEN OTHERS THEN
    IF SQLCODE NOT IN (-2260, -2264) THEN
      RAISE;
    END IF;
END;
/

BEGIN
  EXECUTE IMMEDIATE
    'CREATE INDEX ix_document_idem_recovery ON document_idempotency (status, lease_until)';
EXCEPTION
  WHEN OTHERS THEN
    IF SQLCODE = -955 THEN
      NULL;
    ELSE
      RAISE;
    END IF;
END;
/

COMMENT ON COLUMN document_idempotency.phase IS
  'CLAIMED inicial; FIRMADO recuperable; RECOVERY_REQUIRED tras lease ambiguo; COMPLETED final.';
COMMENT ON COLUMN document_idempotency.request_sha256 IS
  'SHA-256 del payload normalizado para detectar reutilización incompatible de Idempotency-Key.';
COMMENT ON COLUMN document_idempotency.xml_sha256 IS
  'SHA-256 del XML firmado canónico de la operación.';
COMMENT ON COLUMN document_idempotency.lease_until IS
  'Lease del reclamo; un CDC previo al vencimiento exige recuperación, no nueva emisión.';
COMMENT ON COLUMN document.recovery_required IS
  '1 cuando el reintento automático SIFEN se agotó y requiere conciliación.';

COMMIT;
PROMPT === OK: resiliencia fiscal ===
