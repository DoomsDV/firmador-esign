-- Conciliación SIFEN: eventos durables y recuperación segura de documentos.
PROMPT === document_event: datos de envío y recuperación ===

BEGIN
  EXECUTE IMMEDIATE 'ALTER TABLE document_event ADD (environment VARCHAR2(4))';
EXCEPTION WHEN OTHERS THEN IF SQLCODE != -1430 THEN RAISE; END IF;
END;
/
BEGIN
  EXECUTE IMMEDIATE 'ALTER TABLE document_event ADD (event_id VARCHAR2(30))';
EXCEPTION WHEN OTHERS THEN IF SQLCODE != -1430 THEN RAISE; END IF;
END;
/
BEGIN
  EXECUTE IMMEDIATE 'ALTER TABLE document_event ADD (idempotency_key VARCHAR2(128), xml_firmado CLOB, xml_sha256 VARCHAR2(64), response_xml CLOB, recovery_required NUMBER(1) DEFAULT 0 NOT NULL, recovery_reason VARCHAR2(500), updated_at TIMESTAMP(6) WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP NOT NULL)';
EXCEPTION WHEN OTHERS THEN IF SQLCODE != -1430 THEN RAISE; END IF;
END;
/

UPDATE document_event e
   SET environment = NVL((SELECT d.environment FROM document d WHERE d.id_document = e.document_id), 'TEST'),
       event_id = NVL(event_id, 'LEGACY-' || TO_CHAR(e.id_document_event))
 WHERE environment IS NULL OR event_id IS NULL;
/
ALTER TABLE document_event MODIFY (environment NOT NULL, event_id NOT NULL);
/
BEGIN
  EXECUTE IMMEDIATE 'ALTER TABLE document_event ADD CONSTRAINT ck_doc_event_env CHECK (environment IN (''TEST'',''PROD''))';
EXCEPTION WHEN OTHERS THEN IF SQLCODE != -2264 THEN RAISE; END IF;
END;
/
BEGIN
  EXECUTE IMMEDIATE 'ALTER TABLE document_event ADD CONSTRAINT ck_doc_event_recovery CHECK (recovery_required IN (0, 1))';
EXCEPTION WHEN OTHERS THEN IF SQLCODE != -2264 THEN RAISE; END IF;
END;
/
BEGIN
  EXECUTE IMMEDIATE 'ALTER TABLE document_event ADD CONSTRAINT uq_doc_event_id UNIQUE (client_id, event_id)';
EXCEPTION WHEN OTHERS THEN IF SQLCODE != -2261 THEN RAISE; END IF;
END;
/
BEGIN
  EXECUTE IMMEDIATE 'CREATE UNIQUE INDEX uq_doc_event_idempotency ON document_event (CASE WHEN idempotency_key IS NOT NULL THEN client_id END, CASE WHEN idempotency_key IS NOT NULL THEN environment END, idempotency_key)';
EXCEPTION WHEN OTHERS THEN IF SQLCODE NOT IN (-955, -1408) THEN RAISE; END IF;
END;
/

PROMPT === Recompilar paquete y modulos ORDS ===
@@../packages/PKG_ESIGN_DOCUMENT_API.sql
@@../ords/01_esign_module.sql
@@../ords/02_esign_internal_module.sql

PROMPT === Migración conciliación SIFEN finalizada ===
