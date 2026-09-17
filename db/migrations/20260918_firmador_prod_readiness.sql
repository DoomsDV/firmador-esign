-- Endurecimiento de reintentos y claims de eventos fiscales.
PROMPT === Firmador: leases, errores y claims de eventos ===

DECLARE
  l_schema VARCHAR2(128) := SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA');
  PROCEDURE add_col(p_table VARCHAR2, p_definition VARCHAR2) IS
  BEGIN
    EXECUTE IMMEDIATE 'ALTER TABLE ' || p_table || ' ADD (' || p_definition || ')';
  EXCEPTION
    WHEN OTHERS THEN
      IF SQLCODE != -1430 THEN RAISE; END IF;
  END;
BEGIN
  BEGIN
    dbms_rls.enable_policy(l_schema, 'DOCUMENT', 'VPD_DOCUMENT', FALSE);
  EXCEPTION WHEN OTHERS THEN NULL;
  END;
  BEGIN
    dbms_rls.enable_policy(l_schema, 'DOCUMENT_EVENT', 'VPD_DOCUMENT_EVENT', FALSE);
  EXCEPTION WHEN OTHERS THEN NULL;
  END;

  BEGIN
    add_col('DOCUMENT', 'RETRY_LEASE_OWNER VARCHAR2(128)');
    add_col('DOCUMENT', 'RETRY_LEASE_UNTIL TIMESTAMP(6) WITH TIME ZONE');
    add_col('DOCUMENT', 'LAST_RETRY_ERROR VARCHAR2(1000)');
    add_col('DOCUMENT', 'RECONCILED_AT TIMESTAMP(6) WITH TIME ZONE');
    add_col('DOCUMENT_EVENT', 'MENSAJE_RES VARCHAR2(1000)');
    add_col('DOCUMENT_EVENT', 'REQUEST_SHA256 VARCHAR2(64)');
    add_col('DOCUMENT_EVENT', 'LEASE_UNTIL TIMESTAMP(6) WITH TIME ZONE');
  EXCEPTION
    WHEN OTHERS THEN
      BEGIN
        dbms_rls.enable_policy(l_schema, 'DOCUMENT_EVENT', 'VPD_DOCUMENT_EVENT', TRUE);
      EXCEPTION WHEN OTHERS THEN NULL;
      END;
      BEGIN
        dbms_rls.enable_policy(l_schema, 'DOCUMENT', 'VPD_DOCUMENT', TRUE);
      EXCEPTION WHEN OTHERS THEN NULL;
      END;
      RAISE;
  END;

  BEGIN
    dbms_rls.enable_policy(l_schema, 'DOCUMENT_EVENT', 'VPD_DOCUMENT_EVENT', TRUE);
  EXCEPTION WHEN OTHERS THEN NULL;
  END;
  BEGIN
    dbms_rls.enable_policy(l_schema, 'DOCUMENT', 'VPD_DOCUMENT', TRUE);
  EXCEPTION WHEN OTHERS THEN NULL;
  END;
END;
/

UPDATE document_event
   SET recovery_required = 1,
       recovery_reason = NVL(recovery_reason, 'evento legado requiere conciliacion manual'),
       updated_at = SYSTIMESTAMP
 WHERE xml_firmado IS NOT NULL
   AND NVL(estado, 'SIN_ESTADO') NOT IN ('REGISTRADO', 'RECHAZADO');

BEGIN
  EXECUTE IMMEDIATE 'CREATE INDEX ix_document_retry_lease ON document (estado, retry_lease_until, retry_requested, fecha_emision)';
EXCEPTION
  WHEN OTHERS THEN
    IF SQLCODE != -955 THEN RAISE; END IF;
END;
/

PROMPT === Recompilar paquete y modulos ORDS ===
@@../packages/PKG_ESIGN_DOCUMENT_API.sql
@@../ords/02_esign_internal_module.sql

COMMIT;
PROMPT === Firmador: endurecimiento finalizado ===
