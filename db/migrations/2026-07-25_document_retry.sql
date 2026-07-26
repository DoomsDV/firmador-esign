-- Migracion: soporte de reenvio de documentos FIRMADO (fallo transitorio de envio a SET).
-- Agrega el flag de reintento y contadores a document. Idempotente (ignora si ya existen).
-- La VPD sobre document bloquea ALTER ... ADD (DEFAULT NOT NULL) con ORA-28133, asi que se
-- deshabilita la politica durante el ALTER y se re-habilita siempre (incluso ante error).
DECLARE
  l_schema VARCHAR2(128) := sys_context('userenv', 'current_schema');
  PROCEDURE add_col(p_ddl VARCHAR2) IS
  BEGIN
    EXECUTE IMMEDIATE p_ddl;
  EXCEPTION
    WHEN OTHERS THEN
      IF SQLCODE = -1430 THEN NULL; -- ORA-01430: column already exists
      ELSE RAISE; END IF;
  END;
BEGIN
  BEGIN
    dbms_rls.enable_policy(l_schema, 'DOCUMENT', 'VPD_DOCUMENT', FALSE);
  EXCEPTION WHEN OTHERS THEN NULL;
  END;

  BEGIN
    add_col('ALTER TABLE document ADD (retry_requested NUMBER(1) DEFAULT 0 NOT NULL)');
    add_col('ALTER TABLE document ADD (retry_count NUMBER DEFAULT 0 NOT NULL)');
    add_col('ALTER TABLE document ADD (last_retry_at TIMESTAMP(6) WITH TIME ZONE)');
  EXCEPTION
    WHEN OTHERS THEN
      BEGIN dbms_rls.enable_policy(l_schema, 'DOCUMENT', 'VPD_DOCUMENT', TRUE); EXCEPTION WHEN OTHERS THEN NULL; END;
      RAISE;
  END;

  BEGIN
    dbms_rls.enable_policy(l_schema, 'DOCUMENT', 'VPD_DOCUMENT', TRUE);
  EXCEPTION WHEN OTHERS THEN NULL;
  END;
END;
/
