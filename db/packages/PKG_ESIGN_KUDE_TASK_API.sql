-- PKG_ESIGN_KUDE_TASK_API: cola durable de generación KuDE (PDF).
-- Go encola tras APROBADO, reclama con lease y completa tras UploadKude.
-- Bypass VPD en claim (worker atiende todos los tenants); enqueue/complete usan set_client.
CREATE OR REPLACE PACKAGE pkg_esign_kude_task_api AS

  PROCEDURE pr_enqueue(
    p_client_id    IN NUMBER,
    p_cdc          IN VARCHAR2,
    p_payload_json IN CLOB,
    p_out          OUT CLOB
  );

  -- Bypass VPD: lista tareas reclamables (PENDING o lease expirado).
  PROCEDURE pr_claim(
    p_lease_owner   IN VARCHAR2,
    p_lease_seconds IN NUMBER,
    p_limit         IN NUMBER,
    p_out           OUT CLOB
  );

  PROCEDURE pr_complete(
    p_task_id IN NUMBER,
    p_body    IN CLOB,
    p_out     OUT CLOB
  );

  -- Estado de la tarea para un CDC (pending|processing|ready|failed|none).
  PROCEDURE pr_get_status(p_client_id IN NUMBER, p_cdc IN VARCHAR2, p_out OUT CLOB);

END pkg_esign_kude_task_api;
/

CREATE OR REPLACE PACKAGE BODY pkg_esign_kude_task_api AS

  PROCEDURE pr_enqueue(
    p_client_id    IN NUMBER,
    p_cdc          IN VARCHAR2,
    p_payload_json IN CLOB,
    p_out          OUT CLOB
  ) IS
    l_doc_id document.id_document%TYPE;
    l_task_id document_kude_task.id_task%TYPE;
    l_status  document_kude_task.status%TYPE;
    l_data    CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);

    IF p_cdc IS NULL OR LENGTH(TRIM(p_cdc)) = 0 THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request, 'cdc requerido');
    END IF;

    BEGIN
      SELECT id_document INTO l_doc_id
        FROM document WHERE client_id = p_client_id AND cdc = p_cdc;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN
        raise_application_error(pkg_esign_http.c_ora_not_found, 'documento inexistente para ese cdc');
    END;

    BEGIN
      SELECT id_task, status INTO l_task_id, l_status
        FROM document_kude_task WHERE document_id = l_doc_id;

      IF l_status = 'READY' THEN
        SELECT JSON_OBJECT(
                 'task_id' VALUE l_task_id,
                 'status' VALUE l_status,
                 'enqueued' VALUE 'false' FORMAT JSON
                 RETURNING CLOB)
          INTO l_data FROM dual;
        p_out := pkg_esign_util.fn_ok(l_data);
        RETURN;
      END IF;

      IF l_status = 'PROCESSING' THEN
        -- Reclamo activo: no reiniciar; opcionalmente refrescar payload si llega vacío no.
        IF p_payload_json IS NOT NULL AND DBMS_LOB.GETLENGTH(p_payload_json) > 0 THEN
          UPDATE /*+ no_parallel */ document_kude_task
             SET payload_json = p_payload_json,
                 updated_at = SYSTIMESTAMP
           WHERE id_task = l_task_id
             AND payload_json IS NULL;
        END IF;
        SELECT JSON_OBJECT(
                 'task_id' VALUE l_task_id,
                 'status' VALUE l_status,
                 'enqueued' VALUE 'false' FORMAT JSON
                 RETURNING CLOB)
          INTO l_data FROM dual;
        p_out := pkg_esign_util.fn_ok(l_data);
        RETURN;
      END IF;

      -- PENDING o FAILED: reencolar
      UPDATE /*+ no_parallel */ document_kude_task
         SET status = 'PENDING',
             payload_json = NVL(p_payload_json, payload_json),
             lease_owner = NULL,
             lease_until = NULL,
             last_error = CASE WHEN l_status = 'FAILED' THEN NULL ELSE last_error END,
             updated_at = SYSTIMESTAMP,
             completed_at = NULL
       WHERE id_task = l_task_id;

      SELECT JSON_OBJECT(
               'task_id' VALUE l_task_id,
               'status' VALUE 'PENDING',
               'enqueued' VALUE 'true' FORMAT JSON
               RETURNING CLOB)
        INTO l_data FROM dual;
      p_out := pkg_esign_util.fn_ok(l_data);
      RETURN;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN NULL;
    END;

    INSERT /*+ no_parallel */ INTO document_kude_task (
      client_id, document_id, cdc, status, payload_json
    ) VALUES (
      p_client_id, l_doc_id, p_cdc, 'PENDING', p_payload_json
    )
    RETURNING id_task INTO l_task_id;

    SELECT JSON_OBJECT(
             'task_id' VALUE l_task_id,
             'status' VALUE 'PENDING',
             'enqueued' VALUE 'true' FORMAT JSON
             RETURNING CLOB)
      INTO l_data FROM dual;
    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_enqueue;

  PROCEDURE pr_claim(
    p_lease_owner   IN VARCHAR2,
    p_lease_seconds IN NUMBER,
    p_limit         IN NUMBER,
    p_out           OUT CLOB
  ) IS
    l_owner  VARCHAR2(64) := SUBSTR(NVL(TRIM(p_lease_owner), 'worker'), 1, 64);
    l_lease  NUMBER := NVL(p_lease_seconds, 120);
    l_limit  NUMBER := NVL(p_limit, 10);
    l_until  TIMESTAMP(6) WITH TIME ZONE;
    l_data   CLOB;
    l_row    CLOB;
    TYPE t_ids IS TABLE OF NUMBER;
    l_ids    t_ids := t_ids();
  BEGIN
    IF l_lease < 30 THEN l_lease := 30; END IF;
    IF l_lease > 3600 THEN l_lease := 3600; END IF;
    IF l_limit < 1 THEN l_limit := 1; END IF;
    IF l_limit > 50 THEN l_limit := 50; END IF;
    l_until := SYSTIMESTAMP + NUMTODSINTERVAL(l_lease, 'SECOND');

    pkg_esign_session.begin_bootstrap;

    -- Sin ORDER BY/FETCH FIRST: en ADB eso arma una vista y FOR UPDATE SKIP LOCKED
    -- levanta ORA-02014. ROWNUM acota el lote; el worker reintenta en el siguiente ciclo.
    SELECT id_task BULK COLLECT INTO l_ids
      FROM document_kude_task t
     WHERE t.attempts < t.max_attempts
       AND (
             t.status = 'PENDING'
          OR t.status = 'FAILED'
          OR (t.status = 'PROCESSING'
              AND (t.lease_until IS NULL OR t.lease_until < SYSTIMESTAMP))
       )
       AND ROWNUM <= l_limit
     FOR UPDATE SKIP LOCKED;

    IF l_ids.COUNT > 0 THEN
      FORALL i IN 1 .. l_ids.COUNT
        UPDATE /*+ no_parallel */ document_kude_task
           SET status = 'PROCESSING',
               lease_owner = l_owner,
               lease_until = l_until,
               attempts = attempts + 1,
               updated_at = SYSTIMESTAMP,
               last_error = NULL
         WHERE id_task = l_ids(i);

      l_data := TO_CLOB('[');
      FOR i IN 1 .. l_ids.COUNT LOOP
        SELECT JSON_OBJECT(
                 'task_id' VALUE t.id_task,
                 'client_id' VALUE t.client_id,
                 'document_id' VALUE t.document_id,
                 'cdc' VALUE t.cdc,
                 'status' VALUE t.status,
                 'attempts' VALUE t.attempts,
                 'payload_json' VALUE t.payload_json
                 RETURNING CLOB)
          INTO l_row
          FROM document_kude_task t
         WHERE t.id_task = l_ids(i);
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
    p_task_id IN NUMBER,
    p_body    IN CLOB,
    p_out     OUT CLOB
  ) IS
    l_ok     VARCHAR2(10) := LOWER(NVL(json_value(p_body, '$.success'), 'false'));
    l_err    VARCHAR2(2000) := SUBSTR(json_value(p_body, '$.error'), 1, 2000);
    l_status document_kude_task.status%TYPE;
    l_data   CLOB;
  BEGIN
    -- Bypass acotado: el worker completa por task_id sin client context.
    pkg_esign_session.begin_bootstrap;

    BEGIN
      SELECT status INTO l_status
        FROM document_kude_task WHERE id_task = p_task_id
        FOR UPDATE;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN
        pkg_esign_session.end_bootstrap;
        raise_application_error(pkg_esign_http.c_ora_not_found, 'tarea KuDE inexistente');
    END;

    IF l_status = 'READY' THEN
      SELECT JSON_OBJECT('task_id' VALUE p_task_id, 'status' VALUE 'READY' RETURNING CLOB)
        INTO l_data FROM dual;
      pkg_esign_session.end_bootstrap;
      p_out := pkg_esign_util.fn_ok(l_data);
      RETURN;
    END IF;

    IF l_ok IN ('true', '1') THEN
      UPDATE /*+ no_parallel */ document_kude_task
         SET status = 'READY',
             lease_owner = NULL,
             lease_until = NULL,
             last_error = NULL,
             completed_at = SYSTIMESTAMP,
             updated_at = SYSTIMESTAMP
       WHERE id_task = p_task_id;
      l_status := 'READY';
    ELSE
      UPDATE /*+ no_parallel */ document_kude_task t
         SET status = CASE WHEN t.attempts >= t.max_attempts THEN 'FAILED' ELSE 'PENDING' END,
             lease_owner = NULL,
             lease_until = NULL,
             last_error = l_err,
             completed_at = CASE WHEN t.attempts >= t.max_attempts THEN SYSTIMESTAMP END,
             updated_at = SYSTIMESTAMP
       WHERE id_task = p_task_id
      RETURNING status INTO l_status;
    END IF;

    pkg_esign_session.end_bootstrap;

    SELECT JSON_OBJECT('task_id' VALUE p_task_id, 'status' VALUE l_status RETURNING CLOB)
      INTO l_data FROM dual;
    p_out := pkg_esign_util.fn_ok(l_data);
  EXCEPTION
    WHEN OTHERS THEN
      BEGIN pkg_esign_session.end_bootstrap; EXCEPTION WHEN OTHERS THEN NULL; END;
      RAISE;
  END pr_complete;

  PROCEDURE pr_get_status(p_client_id IN NUMBER, p_cdc IN VARCHAR2, p_out OUT CLOB) IS
    l_data CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);
    BEGIN
      SELECT JSON_OBJECT(
               'cdc' VALUE t.cdc,
               'status' VALUE LOWER(t.status),
               'attempts' VALUE t.attempts,
               'last_error' VALUE t.last_error,
               'task_id' VALUE t.id_task
               RETURNING CLOB)
        INTO l_data
        FROM document_kude_task t
        JOIN document d ON d.id_document = t.document_id
       WHERE d.client_id = p_client_id AND d.cdc = p_cdc;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN
        SELECT JSON_OBJECT(
                 'cdc' VALUE p_cdc,
                 'status' VALUE 'none'
                 RETURNING CLOB)
          INTO l_data FROM dual;
    END;
    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_get_status;

END pkg_esign_kude_task_api;
/
