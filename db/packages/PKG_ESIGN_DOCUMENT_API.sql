-- PKG_ESIGN_DOCUMENT_API: persistencia y consulta de documentos emitidos.
-- pr_register_document hace upsert por CDC (Go lo llama tras firmar/enviar). Las consultas
-- (list/get/xml) son para el panel; todos los roles pueden leer, la VPD aisla por client.
CREATE OR REPLACE PACKAGE pkg_esign_document_api AS

  PROCEDURE pr_register_document(p_client_id IN NUMBER, p_body IN CLOB, p_out OUT CLOB);
  PROCEDURE pr_register_event(p_client_id IN NUMBER, p_body IN CLOB, p_out OUT CLOB);

  PROCEDURE pr_list_documents(p_client_id IN NUMBER, p_body IN CLOB, p_out OUT CLOB);
  PROCEDURE pr_get_document(p_client_id IN NUMBER, p_cdc IN VARCHAR2, p_out OUT CLOB);
  PROCEDURE pr_get_xml(p_client_id IN NUMBER, p_cdc IN VARCHAR2, p_out OUT CLOB);
  -- Artefacto XML canónico (APROBADO) con metadatos; para Go / API key.
  PROCEDURE pr_get_xml_artifact(p_client_id IN NUMBER, p_cdc IN VARCHAR2, p_out OUT CLOB);

  -- Lookup por Idempotency-Key (cliente+ambiente). Devuelve found=false si no hay match.
  PROCEDURE pr_find_by_idempotency(p_client_id IN NUMBER, p_body IN CLOB, p_out OUT CLOB);

  -- Reclama atómicamente una Idempotency-Key antes de construir, firmar o enviar.
  -- Devuelve ACQUIRED, IN_FLIGHT o COMPLETED (con el DE previamente emitido).
  PROCEDURE pr_claim_idempotency(p_client_id IN NUMBER, p_body IN CLOB, p_out OUT CLOB);
  -- Libera un reclamo que falló antes de iniciar el envío irreversible a SIFEN.
  PROCEDURE pr_release_idempotency(p_client_id IN NUMBER, p_body IN CLOB, p_out OUT CLOB);

  -- Marca un documento FIRMADO para reenvio (fallo transitorio de envio a SET). El worker
  -- de Go lo reintenta. Solo aplica a estado FIRMADO; otros estados devuelven error.
  PROCEDURE pr_request_retry(p_client_id IN NUMBER, p_cdc IN VARCHAR2, p_out OUT CLOB);

  -- Lista documentos FIRMADO pendientes de reenvio (con xml_firmado). Bypass VPD
  -- (servicio Go). p_mode: 'flagged' (solo retry_requested=1) | 'all' (todos FIRMADO).
  PROCEDURE pr_list_pending_retry(p_mode IN VARCHAR2, p_limit IN NUMBER, p_out OUT CLOB);

  -- Marca un reintento agotado para conciliación manual; no vuelve a emitir el DE.
  PROCEDURE pr_mark_retry_reconciliation(
    p_client_id IN NUMBER,
    p_cdc       IN VARCHAR2,
    p_reason    IN VARCHAR2,
    p_out       OUT CLOB
  );

  -- Documentos APROBADO con XML/QR pero sin tarea KuDE. Bypass VPD para el worker.
  PROCEDURE pr_list_kude_recovery(p_limit IN NUMBER, p_out OUT CLOB);

END pkg_esign_document_api;
/

CREATE OR REPLACE PACKAGE BODY pkg_esign_document_api AS

  PROCEDURE pr_register_document(p_client_id IN NUMBER, p_body IN CLOB, p_out OUT CLOB) IS
    l_cdc  VARCHAR2(44) := json_value(p_body, '$.cdc');
    l_idem VARCHAR2(128) := json_value(p_body, '$.idempotency_key');
    l_doc_id document.id_document%TYPE;
    l_xml  CLOB := json_value(p_body, '$.xml_firmado' RETURNING CLOB);
    l_qr   VARCHAR2(1000) := json_value(p_body, '$.qr_url');
    l_sha  VARCHAR2(64) := LOWER(TRIM(json_value(p_body, '$.xml_sha256')));
    l_size NUMBER := TO_NUMBER(json_value(p_body, '$.xml_size_bytes'));
    l_mime VARCHAR2(100) := NVL(NULLIF(TRIM(json_value(p_body, '$.xml_mime_type')), ''),
                                'application/xml; charset=UTF-8');
    l_exist_xml CLOB;
    l_xml_blob BLOB;
    l_dest_off INTEGER := 1;
    l_src_off INTEGER := 1;
    l_lang_ctx INTEGER := DBMS_LOB.DEFAULT_LANG_CTX;
    l_warning INTEGER;
    l_doc_estado document.estado%TYPE;
    l_estado document.estado%TYPE := UPPER(TRIM(json_value(p_body, '$.estado')));
    l_data CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);

    MERGE /*+ no_parallel */ INTO document d
    USING (SELECT p_client_id AS cid, l_cdc AS cdc FROM dual) s
    ON (d.client_id = s.cid AND d.cdc = s.cdc)
    WHEN MATCHED THEN UPDATE SET
      estado = json_value(p_body, '$.estado'),
      cod_res = json_value(p_body, '$.cod_res'),
      prot_aut = json_value(p_body, '$.prot_aut'),
      mensaje_res = json_value(p_body, '$.mensaje_res'),
      idempotency_key = NVL(d.idempotency_key, NULLIF(TRIM(l_idem), '')),
      retry_requested = CASE
                          WHEN NVL(json_value(p_body, '$.from_retry'), 'false') IN ('true','1')
                          THEN 0
                          WHEN NVL(json_value(p_body, '$.retry_requested'), 'false') IN ('true','1')
                          THEN 1
                          ELSE d.retry_requested
                        END,
      retry_count = CASE
                      WHEN NVL(json_value(p_body, '$.from_retry'), 'false') IN ('true','1')
                      THEN NVL(d.retry_count, 0) + 1 ELSE d.retry_count END,
      last_retry_at = CASE
                        WHEN NVL(json_value(p_body, '$.from_retry'), 'false') IN ('true','1')
                        THEN SYSTIMESTAMP ELSE d.last_retry_at END
    WHEN NOT MATCHED THEN INSERT
      (client_id, environment, tipo_de, cdc, num_documento, establecimiento, punto_expedicion,
       receptor_nombre, receptor_doc, moneda, total_operacion, estado, cod_res, prot_aut, mensaje_res,
       fecha_emision, idempotency_key)
    VALUES
      (p_client_id, json_value(p_body, '$.environment'), json_value(p_body, '$.tipo_de'),
       l_cdc, json_value(p_body, '$.num_documento'),
       json_value(p_body, '$.establecimiento'), json_value(p_body, '$.punto_expedicion'),
       json_value(p_body, '$.receptor_nombre'), json_value(p_body, '$.receptor_doc'),
       json_value(p_body, '$.moneda'), json_value(p_body, '$.total_operacion'),
       json_value(p_body, '$.estado'), json_value(p_body, '$.cod_res'),
       json_value(p_body, '$.prot_aut'), json_value(p_body, '$.mensaje_res'), SYSTIMESTAMP,
       NULLIF(TRIM(l_idem), ''));

    SELECT id_document, estado INTO l_doc_id, l_doc_estado
      FROM document WHERE client_id = p_client_id AND cdc = l_cdc;

    IF NULLIF(TRIM(l_idem), '') IS NOT NULL THEN
      -- FIRMADO es durable, pero no es un resultado terminal: conserva el
      -- claim para que una repetición no genere otro CDC. Solo APROBADO o
      -- RECHAZADO completa la operación lógica.
      MERGE /*+ no_parallel */ INTO document_idempotency i
      USING (
        SELECT p_client_id AS client_id,
               UPPER(json_value(p_body, '$.environment')) AS environment,
               TRIM(l_idem) AS idempotency_key,
               l_cdc AS cdc,
               l_sha AS xml_sha256
          FROM dual
      ) s
      ON (i.client_id = s.client_id
          AND i.environment = s.environment
          AND i.idempotency_key = s.idempotency_key)
      WHEN MATCHED THEN UPDATE SET
        status = CASE
                   WHEN l_estado IN ('APROBADO', 'RECHAZADO', 'CANCELADO') THEN 'COMPLETED'
                   ELSE 'IN_FLIGHT'
                 END,
        cdc = s.cdc,
        phase = CASE
                  WHEN l_estado IN ('APROBADO', 'RECHAZADO', 'CANCELADO') THEN 'COMPLETED'
                  WHEN l_estado = 'FIRMADO' THEN 'FIRMADO'
                  ELSE i.phase
                END,
        xml_sha256 = NVL(s.xml_sha256, i.xml_sha256),
        lease_until = CASE
                        WHEN l_estado IN ('APROBADO', 'RECHAZADO', 'CANCELADO') THEN NULL
                        ELSE SYSTIMESTAMP + NUMTODSINTERVAL(2, 'MINUTE')
                      END,
        recovery_note = CASE
                          WHEN l_estado IN ('APROBADO', 'RECHAZADO', 'CANCELADO') THEN NULL
                          ELSE i.recovery_note
                        END,
        updated_at = SYSTIMESTAMP
      WHEN NOT MATCHED THEN INSERT (
        client_id, environment, idempotency_key, status, cdc, phase, xml_sha256,
        lease_until, created_at, updated_at
      ) VALUES (
        s.client_id, s.environment, s.idempotency_key,
        CASE WHEN l_estado IN ('APROBADO', 'RECHAZADO', 'CANCELADO') THEN 'COMPLETED' ELSE 'IN_FLIGHT' END,
        s.cdc,
        CASE WHEN l_estado IN ('APROBADO', 'RECHAZADO', 'CANCELADO') THEN 'COMPLETED' ELSE 'FIRMADO' END,
        s.xml_sha256,
        CASE
          WHEN l_estado IN ('APROBADO', 'RECHAZADO', 'CANCELADO') THEN NULL
          ELSE SYSTIMESTAMP + NUMTODSINTERVAL(2, 'MINUTE')
        END,
        SYSTIMESTAMP, SYSTIMESTAMP
      );
    END IF;

    -- XML/QR: nunca borrar xml_firmado con NULL ni tocar kude_url.
    -- Si ya hay XML y llega otro distinto para un CDC aprobado -> CONFLICT.
    IF l_xml IS NOT NULL OR l_qr IS NOT NULL THEN
      BEGIN
        SELECT xml_firmado INTO l_exist_xml
          FROM document_xml WHERE document_id = l_doc_id;
      EXCEPTION
        WHEN NO_DATA_FOUND THEN l_exist_xml := NULL;
      END;

      IF l_xml IS NOT NULL AND l_exist_xml IS NOT NULL
         AND DBMS_LOB.COMPARE(l_exist_xml, l_xml) <> 0 THEN
        IF l_doc_estado = 'APROBADO' THEN
          raise_application_error(pkg_esign_http.c_ora_conflict,
            'xml_firmado distinto para CDC aprobado; no se sustituye');
        END IF;
        -- Conservar el XML canónico ya persistido (no sobrescribir con otro distinto).
        l_xml := NULL;
        l_sha := NULL;
        l_size := NULL;
      END IF;

      IF l_xml IS NOT NULL THEN
        DBMS_LOB.CREATETEMPORARY(l_xml_blob, TRUE);
        DBMS_LOB.CONVERTTOBLOB(
          dest_lob     => l_xml_blob,
          src_clob     => l_xml,
          amount       => DBMS_LOB.LOBMAXSIZE,
          dest_offset  => l_dest_off,
          src_offset   => l_src_off,
          blob_csid    => NLS_CHARSET_ID('AL32UTF8'),
          lang_context => l_lang_ctx,
          warning      => l_warning
        );
        l_sha := LOWER(RAWTOHEX(DBMS_CRYPTO.HASH(l_xml_blob, DBMS_CRYPTO.HASH_SH256)));
        IF l_size IS NULL THEN
          l_size := DBMS_LOB.GETLENGTH(l_xml_blob);
        ELSIF l_size <> DBMS_LOB.GETLENGTH(l_xml_blob) THEN
          raise_application_error(pkg_esign_http.c_ora_bad_request,
            'xml_size_bytes no coincide con el XML firmado');
        END IF;
        DBMS_LOB.FREETEMPORARY(l_xml_blob);
      END IF;

      MERGE /*+ no_parallel */ INTO document_xml x
      USING (SELECT l_doc_id AS did FROM dual) s
      ON (x.document_id = s.did)
      WHEN MATCHED THEN UPDATE SET
        xml_firmado = NVL(l_xml, x.xml_firmado),
        qr_url = NVL(l_qr, x.qr_url),
        xml_sha256 = CASE WHEN l_xml IS NOT NULL THEN l_sha ELSE x.xml_sha256 END,
        xml_size_bytes = CASE WHEN l_xml IS NOT NULL THEN l_size ELSE x.xml_size_bytes END,
        xml_mime_type = CASE WHEN l_xml IS NOT NULL THEN l_mime ELSE x.xml_mime_type END,
        xml_captured_at = CASE
                            WHEN l_xml IS NOT NULL AND x.xml_firmado IS NULL THEN SYSTIMESTAMP
                            WHEN l_xml IS NOT NULL AND x.xml_captured_at IS NULL THEN SYSTIMESTAMP
                            ELSE x.xml_captured_at END,
        xml_availability = CASE
                             WHEN NVL(l_xml, x.xml_firmado) IS NOT NULL THEN 'AVAILABLE'
                             ELSE NVL(x.xml_availability, 'MISSING') END
      WHEN NOT MATCHED THEN INSERT (
        document_id, client_id, xml_firmado, qr_url,
        xml_sha256, xml_size_bytes, xml_mime_type, xml_captured_at, xml_availability
      ) VALUES (
        l_doc_id, p_client_id, l_xml, l_qr,
        l_sha, l_size, l_mime,
        CASE WHEN l_xml IS NOT NULL THEN SYSTIMESTAMP END,
        CASE WHEN l_xml IS NOT NULL THEN 'AVAILABLE' ELSE 'MISSING' END
      );
    END IF;

    SELECT JSON_OBJECT('document_id' VALUE l_doc_id, 'cdc' VALUE l_cdc RETURNING CLOB) INTO l_data FROM dual;
    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_register_document;

  PROCEDURE pr_find_by_idempotency(p_client_id IN NUMBER, p_body IN CLOB, p_out OUT CLOB) IS
    l_env  VARCHAR2(4)   := json_value(p_body, '$.environment');
    l_key  VARCHAR2(128) := TRIM(json_value(p_body, '$.idempotency_key'));
    l_data CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);

    IF l_key IS NULL OR l_env IS NULL THEN
      SELECT JSON_OBJECT('found' VALUE FALSE RETURNING CLOB) INTO l_data FROM dual;
      p_out := pkg_esign_util.fn_ok(l_data);
      RETURN;
    END IF;

    BEGIN
      SELECT JSON_OBJECT(
               'found' VALUE TRUE,
               'cdc' VALUE d.cdc,
               'estado' VALUE d.estado,
               'cod_res' VALUE d.cod_res,
               'prot_aut' VALUE d.prot_aut,
               'mensaje_res' VALUE d.mensaje_res,
               'num_documento' VALUE d.num_documento,
               'ambiente' VALUE LOWER(d.environment),
               'qr_url' VALUE x.qr_url
               RETURNING CLOB)
        INTO l_data
        FROM document d
        LEFT JOIN document_xml x ON x.document_id = d.id_document
       WHERE d.client_id = p_client_id
         AND d.environment = l_env
         AND d.idempotency_key = l_key
         AND d.cdc IS NOT NULL
       FETCH FIRST 1 ROWS ONLY;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN
        SELECT JSON_OBJECT('found' VALUE FALSE RETURNING CLOB) INTO l_data FROM dual;
    END;

    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_find_by_idempotency;

  PROCEDURE pr_claim_idempotency(p_client_id IN NUMBER, p_body IN CLOB, p_out OUT CLOB) IS
    l_env             VARCHAR2(4)   := UPPER(TRIM(json_value(p_body, '$.environment')));
    l_key             VARCHAR2(128) := TRIM(json_value(p_body, '$.idempotency_key'));
    l_request_sha256  VARCHAR2(64)  := LOWER(TRIM(json_value(p_body, '$.request_sha256')));
    l_stored_sha256   document_idempotency.request_sha256%TYPE;
    l_status          document_idempotency.status%TYPE;
    l_phase           document_idempotency.phase%TYPE;
    l_lease_until     document_idempotency.lease_until%TYPE;
    l_cdc             document_idempotency.cdc%TYPE;
    l_doc_estado      document.estado%TYPE;
    l_data            CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);

    IF l_key IS NULL OR l_env NOT IN ('TEST', 'PROD') THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request,
        'environment e idempotency_key son obligatorios');
    END IF;

    BEGIN
      INSERT /*+ no_parallel */ INTO document_idempotency (
        client_id, environment, idempotency_key, status, phase, request_sha256,
        lease_until, created_at, updated_at
      ) VALUES (
        p_client_id, l_env, l_key, 'IN_FLIGHT', 'CLAIMED', l_request_sha256,
        SYSTIMESTAMP + NUMTODSINTERVAL(2, 'MINUTE'), SYSTIMESTAMP, SYSTIMESTAMP
      );
    EXCEPTION
      WHEN DUP_VAL_ON_INDEX THEN
        SELECT status, phase, cdc, request_sha256, lease_until
          INTO l_status, l_phase, l_cdc, l_stored_sha256, l_lease_until
          FROM document_idempotency
         WHERE client_id = p_client_id
           AND environment = l_env
           AND idempotency_key = l_key;

        IF l_stored_sha256 IS NOT NULL
           AND l_request_sha256 IS NOT NULL
           AND l_stored_sha256 <> l_request_sha256 THEN
          raise_application_error(pkg_esign_http.c_ora_conflict,
            'Idempotency-Key reutilizada con un payload diferente');
        END IF;

        IF l_status = 'COMPLETED' AND l_cdc IS NOT NULL THEN
          BEGIN
            SELECT JSON_OBJECT(
                     'claim_status' VALUE 'COMPLETED',
                     'found' VALUE 'true' FORMAT JSON,
                     'cdc' VALUE d.cdc,
                     'estado' VALUE d.estado,
                     'cod_res' VALUE d.cod_res,
                     'prot_aut' VALUE d.prot_aut,
                     'mensaje_res' VALUE d.mensaje_res,
                     'num_documento' VALUE d.num_documento,
                     'ambiente' VALUE LOWER(d.environment),
                     'qr_url' VALUE x.qr_url
                     RETURNING CLOB)
              INTO l_data
              FROM document d
              LEFT JOIN document_xml x ON x.document_id = d.id_document
             WHERE d.client_id = p_client_id
               AND d.cdc = l_cdc;
          EXCEPTION
            WHEN NO_DATA_FOUND THEN
              l_data := NULL;
          END;
        END IF;

        IF l_data IS NOT NULL THEN
          p_out := pkg_esign_util.fn_ok(l_data);
          RETURN;
        END IF;

        -- Un claim inicial sin CDC sí puede ser retomado al vencer. Si ya
        -- existe XML/CDC, la operación es ambigua y solo el worker puede
        -- recuperarla con los bytes firmados originales.
        IF l_status = 'IN_FLIGHT'
           AND l_phase = 'CLAIMED'
           AND l_cdc IS NULL
           AND (l_lease_until IS NULL OR l_lease_until < SYSTIMESTAMP) THEN
          UPDATE /*+ no_parallel */ document_idempotency
             SET request_sha256 = NVL(l_request_sha256, request_sha256),
                 lease_until = SYSTIMESTAMP + NUMTODSINTERVAL(2, 'MINUTE'),
                 updated_at = SYSTIMESTAMP
           WHERE client_id = p_client_id
             AND environment = l_env
             AND idempotency_key = l_key;

          SELECT JSON_OBJECT(
                   'claim_status' VALUE 'ACQUIRED',
                   'found' VALUE 'false' FORMAT JSON
                   RETURNING CLOB)
            INTO l_data
            FROM dual;
          p_out := pkg_esign_util.fn_ok(l_data);
          RETURN;
        END IF;

        IF l_status = 'IN_FLIGHT'
           AND (l_lease_until IS NULL OR l_lease_until < SYSTIMESTAMP) THEN
          UPDATE /*+ no_parallel */ document_idempotency
             SET phase = 'RECOVERY_REQUIRED',
                 recovery_note = 'lease vencido tras persistir DE firmado; requiere worker de recuperación',
                 updated_at = SYSTIMESTAMP
           WHERE client_id = p_client_id
             AND environment = l_env
             AND idempotency_key = l_key;
        END IF;

        SELECT JSON_OBJECT(
                 'claim_status' VALUE 'IN_FLIGHT',
                 'found' VALUE 'false' FORMAT JSON,
                 'phase' VALUE NVL(l_phase, 'CLAIMED')
                 RETURNING CLOB)
          INTO l_data
          FROM dual;
        p_out := pkg_esign_util.fn_ok(l_data);
        RETURN;
    END;

    -- Backfill defensivo: una emisión previa al endpoint de claim ya es definitiva.
    BEGIN
      SELECT cdc, estado INTO l_cdc, l_doc_estado
        FROM document
       WHERE client_id = p_client_id
         AND environment = l_env
         AND idempotency_key = l_key
       FETCH FIRST 1 ROWS ONLY;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN
        l_cdc := NULL;
    END;

    IF l_cdc IS NOT NULL THEN
      UPDATE /*+ no_parallel */ document_idempotency
         SET status = CASE
                        WHEN l_doc_estado IN ('APROBADO', 'RECHAZADO', 'CANCELADO') THEN 'COMPLETED'
                        ELSE 'IN_FLIGHT'
                      END,
             cdc = l_cdc,
             phase = CASE
                       WHEN l_doc_estado IN ('APROBADO', 'RECHAZADO', 'CANCELADO') THEN 'COMPLETED'
                       ELSE 'RECOVERY_REQUIRED'
                     END,
             lease_until = CASE
                             WHEN l_doc_estado IN ('APROBADO', 'RECHAZADO', 'CANCELADO') THEN NULL
                             ELSE SYSTIMESTAMP - NUMTODSINTERVAL(1, 'SECOND')
                           END,
             updated_at = SYSTIMESTAMP
       WHERE client_id = p_client_id
         AND environment = l_env
         AND idempotency_key = l_key;

      IF l_doc_estado IN ('APROBADO', 'RECHAZADO', 'CANCELADO') THEN
        SELECT JSON_OBJECT(
                 'claim_status' VALUE 'COMPLETED',
                 'found' VALUE 'true' FORMAT JSON,
                 'cdc' VALUE d.cdc,
                 'estado' VALUE d.estado,
                 'cod_res' VALUE d.cod_res,
                 'prot_aut' VALUE d.prot_aut,
                 'mensaje_res' VALUE d.mensaje_res,
                 'num_documento' VALUE d.num_documento,
                 'ambiente' VALUE LOWER(d.environment),
                 'qr_url' VALUE x.qr_url
                 RETURNING CLOB)
          INTO l_data
          FROM document d
          LEFT JOIN document_xml x ON x.document_id = d.id_document
         WHERE d.client_id = p_client_id
           AND d.cdc = l_cdc;
      ELSE
        SELECT JSON_OBJECT(
                 'claim_status' VALUE 'IN_FLIGHT',
                 'found' VALUE 'false' FORMAT JSON,
                 'phase' VALUE 'RECOVERY_REQUIRED'
                 RETURNING CLOB)
          INTO l_data
          FROM dual;
      END IF;
      p_out := pkg_esign_util.fn_ok(l_data);
      RETURN;
    END IF;

    SELECT JSON_OBJECT(
             'claim_status' VALUE 'ACQUIRED',
             'found' VALUE 'false' FORMAT JSON
             RETURNING CLOB)
      INTO l_data
      FROM dual;
    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_claim_idempotency;

  PROCEDURE pr_release_idempotency(p_client_id IN NUMBER, p_body IN CLOB, p_out OUT CLOB) IS
    l_env     VARCHAR2(4)   := UPPER(TRIM(json_value(p_body, '$.environment')));
    l_key     VARCHAR2(128) := TRIM(json_value(p_body, '$.idempotency_key'));
    l_released NUMBER;
    l_data    CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);

    IF l_key IS NULL OR l_env NOT IN ('TEST', 'PROD') THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request,
        'environment e idempotency_key son obligatorios');
    END IF;

    DELETE /*+ no_parallel */ FROM document_idempotency
     WHERE client_id = p_client_id
       AND environment = l_env
       AND idempotency_key = l_key
       AND status = 'IN_FLIGHT'
       AND phase = 'CLAIMED'
       AND cdc IS NULL;
    l_released := SQL%ROWCOUNT;

    SELECT JSON_OBJECT(
             'released' VALUE CASE WHEN l_released = 1 THEN 'true' ELSE 'false' END FORMAT JSON
             RETURNING CLOB)
      INTO l_data
      FROM dual;
    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_release_idempotency;

  PROCEDURE pr_register_event(p_client_id IN NUMBER, p_body IN CLOB, p_out OUT CLOB) IS
    l_doc_id document.id_document%TYPE;
    l_cdc VARCHAR2(44) := json_value(p_body, '$.cdc');
  BEGIN
    pkg_esign_session.set_client(p_client_id);

    IF l_cdc IS NOT NULL THEN
      BEGIN
        SELECT id_document INTO l_doc_id FROM document WHERE client_id = p_client_id AND cdc = l_cdc;
      EXCEPTION WHEN NO_DATA_FOUND THEN l_doc_id := NULL; END;
    END IF;

    INSERT /*+ no_parallel */ INTO document_event (document_id, client_id, tipo_evento, estado, cod_res, prot_aut, motivo)
    VALUES (l_doc_id, p_client_id, json_value(p_body, '$.tipo_evento'),
            json_value(p_body, '$.estado'), json_value(p_body, '$.cod_res'),
            json_value(p_body, '$.prot_aut'), json_value(p_body, '$.motivo'));

    -- La cancelacion aceptada marca el documento como CANCELADO.
    IF json_value(p_body, '$.tipo_evento') = 'CANCELACION'
       AND json_value(p_body, '$.cod_res') = '0600' AND l_doc_id IS NOT NULL THEN
      UPDATE /*+ no_parallel */ document SET estado = 'CANCELADO' WHERE id_document = l_doc_id;
    END IF;

    p_out := pkg_esign_util.fn_ok;
  END pr_register_event;

  PROCEDURE pr_list_documents(p_client_id IN NUMBER, p_body IN CLOB, p_out OUT CLOB) IS
    l_env    VARCHAR2(4)  := json_value(p_body, '$.environment');
    l_estado VARCHAR2(20) := json_value(p_body, '$.estado');
    l_tipo   NUMBER       := json_value(p_body, '$.tipo');
    l_page   NUMBER       := NVL(json_value(p_body, '$.page'), 1);
    l_size   NUMBER       := NVL(json_value(p_body, '$.pageSize'), 20);
    l_data   CLOB;
    l_meta   CLOB;
    l_total  NUMBER;
  BEGIN
    pkg_esign_session.set_client(p_client_id);

    SELECT COUNT(*) INTO l_total FROM document
     WHERE client_id = p_client_id
       AND (l_env IS NULL OR environment = l_env)
       AND (l_estado IS NULL OR estado = l_estado)
       AND (l_tipo IS NULL OR tipo_de = l_tipo);

    SELECT JSON_ARRAYAGG(
             JSON_OBJECT('cdc' VALUE cdc, 'tipo_de' VALUE tipo_de, 'environment' VALUE environment,
                         'num_documento' VALUE num_documento, 'estado' VALUE estado,
                         'cod_res' VALUE cod_res, 'prot_aut' VALUE prot_aut,
                         'receptor_nombre' VALUE receptor_nombre, 'moneda' VALUE moneda,
                         'total_operacion' VALUE total_operacion,
                         'fecha_emision' VALUE TO_CHAR(fecha_emision, 'YYYY-MM-DD"T"HH24:MI:SSTZH:TZM')
                         RETURNING CLOB) ORDER BY fecha_emision DESC RETURNING CLOB)
      INTO l_data
      FROM (
        SELECT * FROM document
         WHERE client_id = p_client_id
           AND (l_env IS NULL OR environment = l_env)
           AND (l_estado IS NULL OR estado = l_estado)
           AND (l_tipo IS NULL OR tipo_de = l_tipo)
         ORDER BY fecha_emision DESC
         OFFSET (l_page - 1) * l_size ROWS FETCH NEXT l_size ROWS ONLY
      );

    SELECT JSON_OBJECT('page' VALUE l_page, 'pageSize' VALUE l_size, 'total' VALUE l_total RETURNING CLOB)
      INTO l_meta FROM dual;
    p_out := pkg_esign_util.fn_ok_meta(NVL(l_data, TO_CLOB('[]')), l_meta);
  END pr_list_documents;

  PROCEDURE pr_get_document(p_client_id IN NUMBER, p_cdc IN VARCHAR2, p_out OUT CLOB) IS
    l_data CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);
    BEGIN
      SELECT JSON_OBJECT(
               'cdc' VALUE cdc, 'tipo_de' VALUE tipo_de, 'environment' VALUE environment,
               'num_documento' VALUE num_documento, 'establecimiento' VALUE establecimiento,
               'punto_expedicion' VALUE punto_expedicion, 'estado' VALUE estado,
               'cod_res' VALUE cod_res, 'prot_aut' VALUE prot_aut, 'mensaje_res' VALUE mensaje_res,
               'receptor_nombre' VALUE receptor_nombre, 'receptor_doc' VALUE receptor_doc,
               'moneda' VALUE moneda, 'total_operacion' VALUE total_operacion,
               'fecha_emision' VALUE TO_CHAR(fecha_emision, 'YYYY-MM-DD"T"HH24:MI:SSTZH:TZM')
               RETURNING CLOB)
        INTO l_data FROM document WHERE client_id = p_client_id AND cdc = p_cdc;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN raise_application_error(pkg_esign_http.c_ora_not_found, 'documento inexistente');
    END;
    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_get_document;

  -- Panel (JWT): devuelve el CLOB crudo del XML firmado.
  PROCEDURE pr_get_xml(p_client_id IN NUMBER, p_cdc IN VARCHAR2, p_out OUT CLOB) IS
  BEGIN
    pkg_esign_session.set_client(p_client_id);
    BEGIN
      SELECT x.xml_firmado INTO p_out
        FROM document_xml x JOIN document d ON d.id_document = x.document_id
       WHERE d.client_id = p_client_id AND d.cdc = p_cdc
         AND x.xml_firmado IS NOT NULL;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN raise_application_error(pkg_esign_http.c_ora_not_found, 'XML inexistente');
    END;
  END pr_get_xml;

  -- Interno (Go / API key): metadatos + XML canónico. Requiere documento APROBADO
  -- y xml_availability=AVAILABLE. Usado por GET /v1/documents/{cdc}/xml.
  PROCEDURE pr_get_xml_artifact(p_client_id IN NUMBER, p_cdc IN VARCHAR2, p_out OUT CLOB) IS
    l_data CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);
    BEGIN
      SELECT JSON_OBJECT(
               'cdc' VALUE d.cdc,
               'estado' VALUE d.estado,
               'xml_firmado' VALUE x.xml_firmado,
               'xml_sha256' VALUE x.xml_sha256,
               'xml_size_bytes' VALUE x.xml_size_bytes,
               'xml_mime_type' VALUE NVL(x.xml_mime_type, 'application/xml; charset=UTF-8'),
               'xml_captured_at' VALUE TO_CHAR(x.xml_captured_at, 'YYYY-MM-DD"T"HH24:MI:SSTZH:TZM'),
               'xml_availability' VALUE x.xml_availability
               RETURNING CLOB)
        INTO l_data
        FROM document d
        JOIN document_xml x ON x.document_id = d.id_document
       WHERE d.client_id = p_client_id
         AND d.cdc = p_cdc
         AND x.xml_firmado IS NOT NULL;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN
        raise_application_error(pkg_esign_http.c_ora_not_found, 'XML inexistente');
    END;

    IF JSON_VALUE(l_data, '$.estado') <> 'APROBADO' THEN
      raise_application_error(pkg_esign_http.c_ora_conflict,
        'documento no APROBADO; XML solo disponible tras autorizacion SIFEN');
    END IF;

    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_get_xml_artifact;

  PROCEDURE pr_request_retry(p_client_id IN NUMBER, p_cdc IN VARCHAR2, p_out OUT CLOB) IS
    l_estado document.estado%TYPE;
    l_data   CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);
    BEGIN
      SELECT estado INTO l_estado FROM document WHERE client_id = p_client_id AND cdc = p_cdc;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN raise_application_error(pkg_esign_http.c_ora_not_found, 'documento inexistente');
    END;

    IF l_estado <> 'FIRMADO' THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request,
        'solo se puede reenviar un documento en estado FIRMADO (actual: ' || l_estado || ')');
    END IF;

    UPDATE /*+ no_parallel */ document
       SET retry_requested = 1
     WHERE client_id = p_client_id AND cdc = p_cdc;

    SELECT JSON_OBJECT('cdc' VALUE p_cdc, 'retry_requested' VALUE 'true' FORMAT JSON RETURNING CLOB)
      INTO l_data FROM dual;
    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_request_retry;

  PROCEDURE pr_list_pending_retry(p_mode IN VARCHAR2, p_limit IN NUMBER, p_out OUT CLOB) IS
    l_mode  VARCHAR2(20) := LOWER(NVL(TRIM(p_mode), 'flagged'));
    l_limit NUMBER       := NVL(p_limit, 50);
    l_data  CLOB;
  BEGIN
    IF l_mode NOT IN ('flagged', 'all') THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request, 'mode debe ser flagged o all');
    END IF;
    IF l_limit < 1 THEN l_limit := 1; END IF;
    IF l_limit > 200 THEN l_limit := 200; END IF;

    -- Bypass VPD: el worker atiende FIRMADO de todos los tenants.
    pkg_esign_session.begin_bootstrap;

    SELECT JSON_ARRAYAGG(
             JSON_OBJECT(
               'cdc' VALUE d.cdc,
               'client_id' VALUE d.client_id,
               'environment' VALUE d.environment,
               'establecimiento' VALUE d.establecimiento,
               'punto_expedicion' VALUE d.punto_expedicion,
               'num_documento' VALUE d.num_documento,
               'tipo_de' VALUE d.tipo_de,
               'retry_requested' VALUE d.retry_requested,
               'retry_count' VALUE d.retry_count,
               'receptor_nombre' VALUE d.receptor_nombre,
               'receptor_doc' VALUE d.receptor_doc,
               'moneda' VALUE d.moneda,
               'total_operacion' VALUE d.total_operacion,
               'qr_url' VALUE x.qr_url,
               'xml_firmado' VALUE x.xml_firmado,
               'idempotency_key' VALUE d.idempotency_key
               RETURNING CLOB)
             ORDER BY d.retry_requested DESC, NVL(d.last_retry_at, d.fecha_emision) ASC
             RETURNING CLOB)
      INTO l_data
      FROM (
        SELECT d.*
          FROM document d
         WHERE d.estado = 'FIRMADO'
           AND NVL(d.recovery_required, 0) = 0
           AND (
                 d.retry_requested = 1
                 OR (
                   l_mode = 'all'
                   AND d.fecha_emision < SYSTIMESTAMP - NUMTODSINTERVAL(2, 'MINUTE')
                 )
               )
           AND EXISTS (
                 SELECT 1 FROM document_xml x
                  WHERE x.document_id = d.id_document AND x.xml_firmado IS NOT NULL)
         ORDER BY d.retry_requested DESC, NVL(d.last_retry_at, d.fecha_emision) ASC
         FETCH FIRST l_limit ROWS ONLY
      ) d
      JOIN document_xml x ON x.document_id = d.id_document;

    pkg_esign_session.end_bootstrap;
    p_out := pkg_esign_util.fn_ok(NVL(l_data, TO_CLOB('[]')));
  EXCEPTION
    WHEN OTHERS THEN
      BEGIN pkg_esign_session.end_bootstrap; EXCEPTION WHEN OTHERS THEN NULL; END;
      RAISE;
  END pr_list_pending_retry;

  PROCEDURE pr_mark_retry_reconciliation(
    p_client_id IN NUMBER,
    p_cdc       IN VARCHAR2,
    p_reason    IN VARCHAR2,
    p_out       OUT CLOB
  ) IS
    l_data CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);

    UPDATE /*+ no_parallel */ document
       SET recovery_required = 1,
           recovery_reason = SUBSTR(TRIM(p_reason), 1, 500),
           recovery_marked_at = SYSTIMESTAMP,
           retry_requested = 0
     WHERE client_id = p_client_id
       AND cdc = p_cdc
       AND estado = 'FIRMADO';

    IF SQL%ROWCOUNT = 0 THEN
      raise_application_error(pkg_esign_http.c_ora_not_found,
        'documento FIRMADO inexistente para conciliación');
    END IF;

    UPDATE /*+ no_parallel */ document_idempotency
       SET phase = 'RECOVERY_REQUIRED',
           recovery_note = SUBSTR(TRIM(p_reason), 1, 500),
           updated_at = SYSTIMESTAMP
     WHERE client_id = p_client_id
       AND cdc = p_cdc
       AND status = 'IN_FLIGHT';

    SELECT JSON_OBJECT(
             'cdc' VALUE p_cdc,
             'recovery_required' VALUE 'true' FORMAT JSON
             RETURNING CLOB)
      INTO l_data
      FROM dual;
    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_mark_retry_reconciliation;

  PROCEDURE pr_list_kude_recovery(p_limit IN NUMBER, p_out OUT CLOB) IS
    l_limit NUMBER := LEAST(GREATEST(NVL(p_limit, 20), 1), 100);
    l_data  CLOB;
  BEGIN
    -- El worker de KuDE atiende todos los tenants; solo devuelve documentos
    -- APROBADO con XML/QR íntegros y sin tarea durable.
    pkg_esign_session.begin_bootstrap;

    SELECT JSON_ARRAYAGG(
             JSON_OBJECT(
               'client_id' VALUE d.client_id,
               'cdc' VALUE d.cdc,
               'environment' VALUE d.environment,
               'xml_firmado' VALUE x.xml_firmado,
               'qr_url' VALUE x.qr_url
               RETURNING CLOB)
             ORDER BY d.fecha_emision ASC
             RETURNING CLOB)
      INTO l_data
      FROM (
        SELECT d.id_document, d.client_id, d.cdc, d.environment, d.fecha_emision
          FROM document d
          JOIN document_xml x ON x.document_id = d.id_document
         WHERE d.estado = 'APROBADO'
           AND x.xml_firmado IS NOT NULL
           AND x.qr_url IS NOT NULL
           AND NOT EXISTS (
                 SELECT 1
                   FROM document_kude_task t
                  WHERE t.document_id = d.id_document
               )
         ORDER BY d.fecha_emision ASC
         FETCH FIRST l_limit ROWS ONLY
      ) d
      JOIN document_xml x ON x.document_id = d.id_document;

    pkg_esign_session.end_bootstrap;
    p_out := pkg_esign_util.fn_ok(NVL(l_data, TO_CLOB('[]')));
  EXCEPTION
    WHEN OTHERS THEN
      BEGIN pkg_esign_session.end_bootstrap; EXCEPTION WHEN OTHERS THEN NULL; END;
      RAISE;
  END pr_list_kude_recovery;

END pkg_esign_document_api;
/
