-- PKG_ESIGN_WEBHOOK_DELIVERY_API: cola durable de entregas webhook (invoice.ready).
CREATE OR REPLACE PACKAGE pkg_esign_webhook_delivery_api AS

  PROCEDURE pr_enqueue(
    p_client_id IN NUMBER,
    p_cdc       IN VARCHAR2,
    p_out       OUT CLOB
  );

  PROCEDURE pr_claim(
    p_lease_owner   IN VARCHAR2,
    p_lease_seconds IN NUMBER,
    p_limit         IN NUMBER,
    p_out           OUT CLOB
  );

  PROCEDURE pr_complete(
    p_delivery_id IN NUMBER,
    p_body        IN CLOB,
    p_out         OUT CLOB
  );

  -- Reintento explícito de una entrega terminal o un legado PENDING agotado.
  -- No se invoca desde el worker.
  PROCEDURE pr_replay(
    p_delivery_id IN NUMBER,
    p_out         OUT CLOB
  );

  -- Documentos APROBADO con KuDE+XML pero sin entrega DELIVERED (reconciliación).
  PROCEDURE pr_list_recovery(p_limit IN NUMBER, p_out OUT CLOB);

END pkg_esign_webhook_delivery_api;
/

CREATE OR REPLACE PACKAGE BODY pkg_esign_webhook_delivery_api AS

  c_event_ready CONSTANT VARCHAR2(40) := 'invoice.ready';

  PROCEDURE pr_enqueue(
    p_client_id IN NUMBER,
    p_cdc       IN VARCHAR2,
    p_out       OUT CLOB
  ) IS
    l_doc_id      document.id_document%TYPE;
    l_env         document.environment%TYPE;
    l_idem        document.idempotency_key%TYPE;
    l_prot        document.prot_aut%TYPE;
    l_kude_url    document_xml.kude_url%TYPE;
    l_xml_sha     document_xml.xml_sha256%TYPE;
    l_url         client_webhook_endpoint.url%TYPE;
    l_active      client_webhook_endpoint.is_active%TYPE;
    l_delivery_id document_webhook_delivery.id_delivery%TYPE;
    l_event_id    VARCHAR2(80);
    l_uuid        VARCHAR2(64);
    l_status      document_webhook_delivery.status%TYPE;
    l_data        CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);

    IF p_cdc IS NULL OR LENGTH(TRIM(p_cdc)) = 0 THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request, 'cdc requerido');
    END IF;

    SELECT d.id_document, d.environment, d.idempotency_key, d.prot_aut,
           x.kude_url, x.xml_sha256
      INTO l_doc_id, l_env, l_idem, l_prot, l_kude_url, l_xml_sha
      FROM document d
      JOIN document_xml x ON x.document_id = d.id_document
     WHERE d.client_id = p_client_id
       AND d.cdc = p_cdc
       AND d.estado = 'APROBADO'
       AND x.kude_url IS NOT NULL
       AND x.xml_availability = 'AVAILABLE'
       AND x.xml_sha256 IS NOT NULL;

    BEGIN
      SELECT w.url, w.is_active
        INTO l_url, l_active
        FROM client_webhook_endpoint w
       WHERE w.client_id = p_client_id
         AND w.environment = l_env
         AND w.is_active = 1
         AND w.url IS NOT NULL
         AND w.secret_ciphertext IS NOT NULL;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN
        SELECT JSON_OBJECT(
                 'enqueued' VALUE 'false' FORMAT JSON,
                 'reason' VALUE 'webhook_inactive'
                 RETURNING CLOB)
          INTO l_data FROM dual;
        p_out := pkg_esign_util.fn_ok(l_data);
        RETURN;
    END;

    l_event_id := 'document.' || c_event_ready || '.' || p_cdc;
    l_uuid := LOWER(REPLACE(RAWTOHEX(SYS_GUID()), '-', ''));

    BEGIN
      SELECT id_delivery, status
        INTO l_delivery_id, l_status
        FROM document_webhook_delivery
       WHERE document_id = l_doc_id
         AND event_type = c_event_ready;

      IF l_status = 'DELIVERED' THEN
        SELECT JSON_OBJECT(
                 'delivery_id' VALUE l_delivery_id,
                 'status' VALUE l_status,
                 'enqueued' VALUE 'false' FORMAT JSON
                 RETURNING CLOB)
          INTO l_data FROM dual;
        p_out := pkg_esign_util.fn_ok(l_data);
        RETURN;
      END IF;

      IF l_status = 'PROCESSING' THEN
        SELECT JSON_OBJECT(
                 'delivery_id' VALUE l_delivery_id,
                 'status' VALUE l_status,
                 'enqueued' VALUE 'false' FORMAT JSON
                 RETURNING CLOB)
          INTO l_data FROM dual;
        p_out := pkg_esign_util.fn_ok(l_data);
        RETURN;
      END IF;

      IF l_status = 'FAILED' THEN
        SELECT JSON_OBJECT(
                 'delivery_id' VALUE l_delivery_id,
                 'status' VALUE l_status,
                 'enqueued' VALUE 'false' FORMAT JSON,
                 'reason' VALUE 'requires_manual_replay'
                 RETURNING CLOB)
          INTO l_data FROM dual;
        p_out := pkg_esign_util.fn_ok(l_data);
        RETURN;
      END IF;

      UPDATE /*+ no_parallel */ document_webhook_delivery
         SET target_url = l_url,
             updated_at = SYSTIMESTAMP
       WHERE id_delivery = l_delivery_id
         AND status = 'PENDING';

      SELECT JSON_OBJECT(
               'delivery_id' VALUE l_delivery_id,
               'status' VALUE 'PENDING',
               'enqueued' VALUE 'true' FORMAT JSON
               RETURNING CLOB)
        INTO l_data FROM dual;
      p_out := pkg_esign_util.fn_ok(l_data);
      RETURN;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN NULL;
    END;

    INSERT /*+ no_parallel */ INTO document_webhook_delivery (
      client_id, document_id, cdc, environment, event_type,
      event_id, delivery_uuid, target_url, status
    ) VALUES (
      p_client_id, l_doc_id, p_cdc, l_env, c_event_ready,
      l_event_id, l_uuid, l_url, 'PENDING'
    )
    RETURNING id_delivery INTO l_delivery_id;

    SELECT JSON_OBJECT(
             'delivery_id' VALUE l_delivery_id,
             'status' VALUE 'PENDING',
             'enqueued' VALUE 'true' FORMAT JSON
             RETURNING CLOB)
      INTO l_data FROM dual;
    p_out := pkg_esign_util.fn_ok(l_data);
  EXCEPTION
    WHEN NO_DATA_FOUND THEN
      raise_application_error(pkg_esign_http.c_ora_not_found,
        'documento no listo para webhook (APROBADO + KuDE + XML)');
  END pr_enqueue;

  PROCEDURE pr_claim(
    p_lease_owner   IN VARCHAR2,
    p_lease_seconds IN NUMBER,
    p_limit         IN NUMBER,
    p_out           OUT CLOB
  ) IS
    l_owner VARCHAR2(64) := SUBSTR(NVL(TRIM(p_lease_owner), 'webhook-worker'), 1, 64);
    l_lease NUMBER := NVL(p_lease_seconds, 120);
    l_limit NUMBER := NVL(p_limit, 10);
    l_until TIMESTAMP(6) WITH TIME ZONE;
    l_data  CLOB;
    l_row   CLOB;
    TYPE t_ids IS TABLE OF NUMBER;
    l_ids   t_ids := t_ids();
  BEGIN
    IF l_lease < 30 THEN l_lease := 30; END IF;
    IF l_lease > 3600 THEN l_lease := 3600; END IF;
    IF l_limit < 1 THEN l_limit := 1; END IF;
    IF l_limit > 50 THEN l_limit := 50; END IF;
    l_until := SYSTIMESTAMP + NUMTODSINTERVAL(l_lease, 'SECOND');

    pkg_esign_session.begin_bootstrap;

    SELECT id_delivery BULK COLLECT INTO l_ids
      FROM document_webhook_delivery t
     WHERE t.attempts < t.max_attempts
       AND (
             (t.status = 'PENDING'
              AND (t.lease_until IS NULL OR t.lease_until < SYSTIMESTAMP))
          OR (t.status = 'PROCESSING'
              AND (t.lease_until IS NULL OR t.lease_until < SYSTIMESTAMP))
       )
       AND ROWNUM <= l_limit
     FOR UPDATE SKIP LOCKED;

    IF l_ids.COUNT > 0 THEN
      FORALL i IN 1 .. l_ids.COUNT
        UPDATE /*+ no_parallel */ document_webhook_delivery
           SET status = 'PROCESSING',
               lease_owner = l_owner,
               lease_until = l_until,
               attempts = attempts + 1,
               updated_at = SYSTIMESTAMP
         WHERE id_delivery = l_ids(i);

      l_data := TO_CLOB('[');
      FOR i IN 1 .. l_ids.COUNT LOOP
        SELECT JSON_OBJECT(
                 'delivery_id' VALUE w.id_delivery,
                 'client_id' VALUE w.client_id,
                 'document_id' VALUE w.document_id,
                 'cdc' VALUE w.cdc,
                 'environment' VALUE w.environment,
                 'event_type' VALUE w.event_type,
                 'event_id' VALUE w.event_id,
                 'delivery_uuid' VALUE w.delivery_uuid,
                 'target_url' VALUE w.target_url,
                 'attempts' VALUE w.attempts,
                 'idempotency_key' VALUE d.idempotency_key,
                 'prot_aut' VALUE d.prot_aut,
                 'kude_url' VALUE x.kude_url,
                 'xml_sha256' VALUE x.xml_sha256,
                 'xml_size_bytes' VALUE x.xml_size_bytes,
                 'xml_mime_type' VALUE x.xml_mime_type
                 RETURNING CLOB)
          INTO l_row
          FROM document_webhook_delivery w
          JOIN document d ON d.id_document = w.document_id
          JOIN document_xml x ON x.document_id = w.document_id
         WHERE w.id_delivery = l_ids(i);
        IF i > 1 THEN
          DBMS_LOB.APPEND(l_data, TO_CLOB(','));
        END IF;
        DBMS_LOB.APPEND(l_data, l_row);
      END LOOP;
      DBMS_LOB.APPEND(l_data, TO_CLOB(']'));
    END IF;

    pkg_esign_session.end_bootstrap;
    p_out := pkg_esign_util.fn_ok(NVL(l_data, TO_CLOB('[]')));
  EXCEPTION
    WHEN OTHERS THEN
      BEGIN pkg_esign_session.end_bootstrap; EXCEPTION WHEN OTHERS THEN NULL; END;
      RAISE;
  END pr_claim;

  PROCEDURE pr_complete(
    p_delivery_id IN NUMBER,
    p_body        IN CLOB,
    p_out         OUT CLOB
  ) IS
    l_ok           VARCHAR2(10) := LOWER(NVL(json_value(p_body, '$.success'), 'false'));
    l_http_status  NUMBER := json_value(p_body, '$.http_status' RETURNING NUMBER);
    l_err          VARCHAR2(2000) := SUBSTR(json_value(p_body, '$.error'), 1, 2000);
    l_retryable    VARCHAR2(10) := LOWER(NVL(json_value(p_body, '$.retryable'), 'true'));
    l_status       document_webhook_delivery.status%TYPE;
    l_data         CLOB;
  BEGIN
    pkg_esign_session.begin_bootstrap;

    BEGIN
      SELECT status INTO l_status
        FROM document_webhook_delivery
       WHERE id_delivery = p_delivery_id
       FOR UPDATE;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN
        pkg_esign_session.end_bootstrap;
        raise_application_error(pkg_esign_http.c_ora_not_found, 'entrega webhook inexistente');
    END;

    IF l_status = 'DELIVERED' THEN
      SELECT JSON_OBJECT('delivery_id' VALUE p_delivery_id, 'status' VALUE 'DELIVERED' RETURNING CLOB)
        INTO l_data FROM dual;
      pkg_esign_session.end_bootstrap;
      p_out := pkg_esign_util.fn_ok(l_data);
      RETURN;
    END IF;

    IF l_ok IN ('true', '1') THEN
      UPDATE /*+ no_parallel */ document_webhook_delivery
         SET status = 'DELIVERED',
             lease_owner = NULL,
             lease_until = NULL,
             last_http_status = l_http_status,
             last_error = NULL,
             delivered_at = SYSTIMESTAMP,
             updated_at = SYSTIMESTAMP
       WHERE id_delivery = p_delivery_id;
      l_status := 'DELIVERED';
    ELSE
      UPDATE /*+ no_parallel */ document_webhook_delivery w
         SET status = CASE
                        WHEN l_retryable IN ('false', '0') THEN 'FAILED'
                        WHEN w.attempts >= w.max_attempts THEN 'FAILED'
                        ELSE 'PENDING'
                      END,
             lease_owner = NULL,
             -- Para PENDING, lease_until funciona como next_attempt_at. Evita
             -- ampliar el esquema y bloquea el claim hasta el backoff.
             lease_until = CASE
                             WHEN l_retryable IN ('true', '1')
                                  AND w.attempts < w.max_attempts
                               THEN SYSTIMESTAMP + NUMTODSINTERVAL(
                                      LEAST(30 * POWER(2, GREATEST(w.attempts - 1, 0)), 900),
                                      'SECOND'
                                    )
                             ELSE NULL
                           END,
             last_http_status = l_http_status,
             last_error = l_err,
             delivered_at = NULL,
             updated_at = SYSTIMESTAMP
       WHERE id_delivery = p_delivery_id
      RETURNING status INTO l_status;
    END IF;

    pkg_esign_session.end_bootstrap;

    SELECT JSON_OBJECT('delivery_id' VALUE p_delivery_id, 'status' VALUE l_status RETURNING CLOB)
      INTO l_data FROM dual;
    p_out := pkg_esign_util.fn_ok(l_data);
  EXCEPTION
    WHEN OTHERS THEN
      BEGIN pkg_esign_session.end_bootstrap; EXCEPTION WHEN OTHERS THEN NULL; END;
      RAISE;
  END pr_complete;

  PROCEDURE pr_replay(
    p_delivery_id IN NUMBER,
    p_out         OUT CLOB
  ) IS
    l_status document_webhook_delivery.status%TYPE;
    l_attempts document_webhook_delivery.attempts%TYPE;
    l_max_attempts document_webhook_delivery.max_attempts%TYPE;
    l_data   CLOB;
  BEGIN
    pkg_esign_session.begin_bootstrap;

    SELECT status, attempts, max_attempts
      INTO l_status, l_attempts, l_max_attempts
      FROM document_webhook_delivery
     WHERE id_delivery = p_delivery_id
     FOR UPDATE;

    IF l_status <> 'FAILED'
       AND NOT (l_status = 'PENDING' AND l_attempts >= l_max_attempts) THEN
      pkg_esign_session.end_bootstrap;
      raise_application_error(pkg_esign_http.c_ora_bad_request,
        'solo se puede replay una entrega FAILED o PENDING agotada');
    END IF;

    UPDATE /*+ no_parallel */ document_webhook_delivery
       SET status = 'PENDING',
           attempts = 0,
           lease_owner = NULL,
           lease_until = NULL,
           delivered_at = NULL,
           last_error = SUBSTR(
             'replay manual solicitado; último error: ' || NVL(last_error, 'sin detalle'),
             1,
             2000
           ),
           updated_at = SYSTIMESTAMP
     WHERE id_delivery = p_delivery_id;

    pkg_esign_session.end_bootstrap;
    SELECT JSON_OBJECT('delivery_id' VALUE p_delivery_id, 'status' VALUE 'PENDING' RETURNING CLOB)
      INTO l_data FROM dual;
    p_out := pkg_esign_util.fn_ok(l_data);
  EXCEPTION
    WHEN NO_DATA_FOUND THEN
      BEGIN pkg_esign_session.end_bootstrap; EXCEPTION WHEN OTHERS THEN NULL; END;
      raise_application_error(pkg_esign_http.c_ora_not_found, 'entrega webhook inexistente');
    WHEN OTHERS THEN
      BEGIN pkg_esign_session.end_bootstrap; EXCEPTION WHEN OTHERS THEN NULL; END;
      RAISE;
  END pr_replay;

  PROCEDURE pr_list_recovery(p_limit IN NUMBER, p_out OUT CLOB) IS
    l_limit NUMBER := LEAST(GREATEST(NVL(p_limit, 20), 1), 100);
    l_data  CLOB;
  BEGIN
    pkg_esign_session.begin_bootstrap;

    SELECT JSON_ARRAYAGG(
             JSON_OBJECT(
               'client_id' VALUE d.client_id,
               'cdc' VALUE d.cdc
               RETURNING CLOB)
             ORDER BY d.fecha_emision ASC
             RETURNING CLOB)
      INTO l_data
      FROM (
        SELECT d.client_id, d.cdc, d.fecha_emision
          FROM document d
          JOIN document_xml x ON x.document_id = d.id_document
          JOIN client_webhook_endpoint w
            ON w.client_id = d.client_id AND w.environment = d.environment
         WHERE d.estado = 'APROBADO'
           AND x.kude_url IS NOT NULL
           AND x.xml_availability = 'AVAILABLE'
           AND w.is_active = 1
           AND w.url IS NOT NULL
           AND w.secret_ciphertext IS NOT NULL
           AND NOT EXISTS (
                 SELECT 1
                   FROM document_webhook_delivery wh
                  WHERE wh.document_id = d.id_document
                    AND wh.event_type = c_event_ready
                    AND wh.status = 'DELIVERED'
               )
         ORDER BY d.fecha_emision ASC
         FETCH FIRST l_limit ROWS ONLY
      ) d;

    pkg_esign_session.end_bootstrap;
    p_out := pkg_esign_util.fn_ok(NVL(l_data, TO_CLOB('[]')));
  EXCEPTION
    WHEN OTHERS THEN
      BEGIN pkg_esign_session.end_bootstrap; EXCEPTION WHEN OTHERS THEN NULL; END;
      RAISE;
  END pr_list_recovery;

END pkg_esign_webhook_delivery_api;
/
