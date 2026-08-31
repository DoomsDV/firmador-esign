-- Idempotency-Key por tenant/ambiente en document (evita 2do CDC en reintentos).
PROMPT === document.idempotency_key ===

DECLARE
    v_count NUMBER;
BEGIN
    SELECT COUNT(*) INTO v_count
      FROM user_tab_columns
     WHERE table_name = 'DOCUMENT' AND column_name = 'IDEMPOTENCY_KEY';
    IF v_count = 0 THEN
        EXECUTE IMMEDIATE 'ALTER TABLE document ADD (idempotency_key VARCHAR2(128))';
    END IF;
END;
/

BEGIN
    EXECUTE IMMEDIATE
      'CREATE UNIQUE INDEX uq_document_idempotency ON document (
         CASE WHEN idempotency_key IS NOT NULL THEN client_id END,
         CASE WHEN idempotency_key IS NOT NULL THEN environment END,
         idempotency_key)';
EXCEPTION
    WHEN OTHERS THEN
        IF SQLCODE NOT IN (-955, -1408) THEN RAISE; END IF;
END;
/

COMMENT ON COLUMN document.idempotency_key IS
  'Clave Idempotency-Key del emisor (ej. INV-{invoice_id}). Unica por client+environment.';

COMMIT;
PROMPT === OK: document.idempotency_key ===
