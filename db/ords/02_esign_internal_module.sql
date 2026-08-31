-- Modulo ORDS 'esign_internal' (/internal/v1/) para el servicio Go. Cada handler valida el
-- header X-Service-Token contra app_parameter.SERVICE_TOKEN antes de tocar los paquetes. La
-- API key del comercio (en el body) resuelve el client_id + environment (pr_resolve_key).
-- Convenciones de los handlers:
--   * El cuerpo se lee UNA sola vez en l_body := :body_text (CLOB); :body es BLOB y no liga
--     con json_value/CLOB, y referenciar :body_text mas de una vez en el mismo handler falla.
--   * Los codigos/mensajes HTTP salen de PKG_ESIGN_HTTP (sin numeros sueltos).
--   * Las llamadas a los paquetes usan notacion de parametros nombrados (p_x => valor).
BEGIN
  BEGIN
    ords.delete_module(p_module_name => 'esign_internal');
  EXCEPTION WHEN OTHERS THEN NULL;
  END;

  ords.define_module(
    p_module_name    => 'esign_internal',
    p_base_path      => '/internal/v1/',
    p_items_per_page => 25,
    p_status         => 'PUBLISHED',
    p_comments       => 'API interna Go<->ORDS (X-Service-Token).');

  ---------------------------------------------------------------------------
  -- CONTEXT: api key -> client_id + environment + emisor + establecimientos + refs.
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign_internal', p_pattern => 'context');
  ords.define_handler(
    p_module_name => 'esign_internal', p_pattern => 'context', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body   CLOB := :body_text;
    l_out    CLOB;
    l_cid    NUMBER;
    l_env    VARCHAR2(4);
    l_status VARCHAR2(20);
BEGIN
    IF NOT pkg_esign_util.fn_service_token_ok(owa_util.get_cgi_env('X-Service-Token')) THEN
        pkg_esign_http.pr_error(
            p_status  => pkg_esign_http.c_unauthorized,
            p_reason  => pkg_esign_http.m_unauthorized,
            p_code    => pkg_esign_http.e_unauthorized,
            p_message => pkg_esign_http.msg_service_token
        );
        RETURN;
    END IF;

    pkg_esign_apikey_api.pr_resolve_key(
        p_api_key     => json_value(l_body, '$.api_key'),
        p_client_id   => l_cid,
        p_environment => l_env,
        p_status      => l_status
    );

    IF l_status <> 'ACTIVE' THEN
        pkg_esign_http.pr_error(
            p_status  => pkg_esign_http.c_forbidden,
            p_reason  => pkg_esign_http.m_forbidden,
            p_code    => pkg_esign_http.e_key_revoked,
            p_message => pkg_esign_http.msg_key_inactive
        );
        RETURN;
    END IF;

    pkg_esign_client_api.pr_get_internal_context(
        p_client_id   => l_cid,
        p_environment => l_env,
        p_out         => l_out
    );
    htp.p(l_out);
END;]');

  ---------------------------------------------------------------------------
  -- NEXT-NUMBER: correlativo con bloqueo.
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign_internal', p_pattern => 'next-number');
  ords.define_handler(
    p_module_name => 'esign_internal', p_pattern => 'next-number', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_num  NUMBER;
    l_data CLOB;
BEGIN
    IF NOT pkg_esign_util.fn_service_token_ok(owa_util.get_cgi_env('X-Service-Token')) THEN
        pkg_esign_http.pr_error(
            p_status  => pkg_esign_http.c_unauthorized,
            p_reason  => pkg_esign_http.m_unauthorized,
            p_code    => pkg_esign_http.e_unauthorized,
            p_message => pkg_esign_http.msg_service_token
        );
        RETURN;
    END IF;

    pkg_esign_client_api.pr_next_document_number(
        p_client_id       => TO_NUMBER(json_value(l_body, '$.client_id')),
        p_environment     => json_value(l_body, '$.environment'),
        p_establecimiento => json_value(l_body, '$.establecimiento'),
        p_punto           => json_value(l_body, '$.punto_expedicion'),
        p_tipo_de         => TO_NUMBER(json_value(l_body, '$.tipo_de')),
        p_number          => l_num
    );
    COMMIT;

    -- JSON_OBJECT ... RETURNING CLOB es solo-SQL en PL/SQL: usar SELECT ... INTO.
    SELECT JSON_OBJECT('number' VALUE l_num RETURNING CLOB) INTO l_data FROM dual;
    htp.p(pkg_esign_util.fn_ok(l_data));
END;]');

  ---------------------------------------------------------------------------
  -- CERTIFICATE: blobs cifrados (hex) para Go.
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign_internal', p_pattern => 'certificate');
  ords.define_handler(
    p_module_name => 'esign_internal', p_pattern => 'certificate', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    IF NOT pkg_esign_util.fn_service_token_ok(owa_util.get_cgi_env('X-Service-Token')) THEN
        pkg_esign_http.pr_error(
            p_status  => pkg_esign_http.c_unauthorized,
            p_reason  => pkg_esign_http.m_unauthorized,
            p_code    => pkg_esign_http.e_unauthorized,
            p_message => pkg_esign_http.msg_service_token
        );
        RETURN;
    END IF;

    pkg_esign_cert_api.pr_get_certificate_json(
        p_client_id => TO_NUMBER(json_value(l_body, '$.client_id')),
        p_out       => l_out
    );
    htp.p(l_out);
END;]');

  ---------------------------------------------------------------------------
  -- CSC: CSC cifrado (hex) del ambiente para el QR (Go lo descifra en memoria).
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign_internal', p_pattern => 'csc');
  ords.define_handler(
    p_module_name => 'esign_internal', p_pattern => 'csc', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    IF NOT pkg_esign_util.fn_service_token_ok(owa_util.get_cgi_env('X-Service-Token')) THEN
        pkg_esign_http.pr_error(
            p_status  => pkg_esign_http.c_unauthorized,
            p_reason  => pkg_esign_http.m_unauthorized,
            p_code    => pkg_esign_http.e_unauthorized,
            p_message => pkg_esign_http.msg_service_token
        );
        RETURN;
    END IF;

    pkg_esign_client_api.pr_get_csc_json(
        p_client_id   => TO_NUMBER(json_value(l_body, '$.client_id')),
        p_environment => json_value(l_body, '$.environment'),
        p_out         => l_out
    );
    htp.p(l_out);
END;]');

  ---------------------------------------------------------------------------
  -- DOCUMENTS: registrar documento emitido (estado/XML/QR).
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign_internal', p_pattern => 'documents');
  ords.define_handler(
    p_module_name => 'esign_internal', p_pattern => 'documents', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    IF NOT pkg_esign_util.fn_service_token_ok(owa_util.get_cgi_env('X-Service-Token')) THEN
        pkg_esign_http.pr_error(
            p_status  => pkg_esign_http.c_unauthorized,
            p_reason  => pkg_esign_http.m_unauthorized,
            p_code    => pkg_esign_http.e_unauthorized,
            p_message => pkg_esign_http.msg_service_token
        );
        RETURN;
    END IF;

    pkg_esign_document_api.pr_register_document(
        p_client_id => TO_NUMBER(json_value(l_body, '$.client_id')),
        p_body      => l_body,
        p_out       => l_out
    );
    COMMIT;
    htp.p(l_out);
END;]');

  ---------------------------------------------------------------------------
  -- DOCUMENTS/BY-IDEMPOTENCY: lookup por Idempotency-Key (retry sin nuevo CDC).
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign_internal', p_pattern => 'documents/by-idempotency');
  ords.define_handler(
    p_module_name => 'esign_internal', p_pattern => 'documents/by-idempotency', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    IF NOT pkg_esign_util.fn_service_token_ok(owa_util.get_cgi_env('X-Service-Token')) THEN
        pkg_esign_http.pr_error(
            p_status  => pkg_esign_http.c_unauthorized,
            p_reason  => pkg_esign_http.m_unauthorized,
            p_code    => pkg_esign_http.e_unauthorized,
            p_message => pkg_esign_http.msg_service_token
        );
        RETURN;
    END IF;

    pkg_esign_document_api.pr_find_by_idempotency(
        p_client_id => TO_NUMBER(json_value(l_body, '$.client_id')),
        p_body      => l_body,
        p_out       => l_out
    );
    htp.p(l_out);
END;]');

  ---------------------------------------------------------------------------
  -- KUDE: Go sube el PDF ya renderizado (Gotenberg) al bucket OCI y lo asocia al CDC.
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign_internal', p_pattern => 'kude');
  ords.define_handler(
    p_module_name => 'esign_internal', p_pattern => 'kude', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    IF NOT pkg_esign_util.fn_service_token_ok(owa_util.get_cgi_env('X-Service-Token')) THEN
        pkg_esign_http.pr_error(
            p_status  => pkg_esign_http.c_unauthorized,
            p_reason  => pkg_esign_http.m_unauthorized,
            p_code    => pkg_esign_http.e_unauthorized,
            p_message => pkg_esign_http.msg_service_token
        );
        RETURN;
    END IF;

    pkg_esign_kude_api.pr_store_kude(
        p_client_id => TO_NUMBER(json_value(l_body, '$.client_id')),
        p_cdc       => json_value(l_body, '$.cdc'),
        p_pdf_hex   => json_value(l_body, '$.pdf_hex' RETURNING CLOB),
        p_out       => l_out
    );
    COMMIT;
    htp.p(l_out);
END;]');

  ---------------------------------------------------------------------------
  -- KUDE/STATUS: Go consulta kude_url + estado (pending|ready) para exponerlo
  -- al plano de emision (sk_). Reutiliza pkg_esign_kude_api.pr_get_kude, ya
  -- usado por el panel (JWT); aqui el caller es el propio servicio Go.
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign_internal', p_pattern => 'kude/status');
  ords.define_handler(
    p_module_name => 'esign_internal', p_pattern => 'kude/status', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    IF NOT pkg_esign_util.fn_service_token_ok(owa_util.get_cgi_env('X-Service-Token')) THEN
        pkg_esign_http.pr_error(
            p_status  => pkg_esign_http.c_unauthorized,
            p_reason  => pkg_esign_http.m_unauthorized,
            p_code    => pkg_esign_http.e_unauthorized,
            p_message => pkg_esign_http.msg_service_token
        );
        RETURN;
    END IF;

    pkg_esign_kude_api.pr_get_kude(
        p_client_id => TO_NUMBER(json_value(l_body, '$.client_id')),
        p_cdc       => json_value(l_body, '$.cdc'),
        p_out       => l_out
    );
    htp.p(l_out);
END;]');

  ---------------------------------------------------------------------------
  -- DOCUMENTS/PENDING-RETRY: lista FIRMADO con xml (worker Go).
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign_internal', p_pattern => 'documents/pending-retry');
  ords.define_handler(
    p_module_name => 'esign_internal', p_pattern => 'documents/pending-retry', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    IF NOT pkg_esign_util.fn_service_token_ok(owa_util.get_cgi_env('X-Service-Token')) THEN
        pkg_esign_http.pr_error(
            p_status  => pkg_esign_http.c_unauthorized,
            p_reason  => pkg_esign_http.m_unauthorized,
            p_code    => pkg_esign_http.e_unauthorized,
            p_message => pkg_esign_http.msg_service_token
        );
        RETURN;
    END IF;

    pkg_esign_document_api.pr_list_pending_retry(
        p_mode  => NVL(json_value(l_body, '$.mode'), 'flagged'),
        p_limit => NVL(TO_NUMBER(json_value(l_body, '$.limit')), 50),
        p_out   => l_out
    );
    htp.p(l_out);
END;]');

  ---------------------------------------------------------------------------
  -- CERTIFICATE/STORE: persistir P12 cifrado (mediacion Go del panel).
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign_internal', p_pattern => 'certificate/store');
  ords.define_handler(
    p_module_name => 'esign_internal', p_pattern => 'certificate/store', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    IF NOT pkg_esign_util.fn_service_token_ok(owa_util.get_cgi_env('X-Service-Token')) THEN
        pkg_esign_http.pr_error(
            p_status  => pkg_esign_http.c_unauthorized,
            p_reason  => pkg_esign_http.m_unauthorized,
            p_code    => pkg_esign_http.e_unauthorized,
            p_message => pkg_esign_http.msg_service_token
        );
        RETURN;
    END IF;

    pkg_esign_cert_api.pr_put_certificate(
        p_client_id => TO_NUMBER(json_value(l_body, '$.client_id')),
        p_role      => NVL(json_value(l_body, '$.role'), 'owner'),
        p_body      => l_body,
        p_out       => l_out
    );
    COMMIT;
    htp.p(l_out);
END;]');

  ---------------------------------------------------------------------------
  -- ENVIRONMENTS/STORE: persistir timbrado + CSC cifrado (mediacion Go).
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign_internal', p_pattern => 'environments/store');
  ords.define_handler(
    p_module_name => 'esign_internal', p_pattern => 'environments/store', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    IF NOT pkg_esign_util.fn_service_token_ok(owa_util.get_cgi_env('X-Service-Token')) THEN
        pkg_esign_http.pr_error(
            p_status  => pkg_esign_http.c_unauthorized,
            p_reason  => pkg_esign_http.m_unauthorized,
            p_code    => pkg_esign_http.e_unauthorized,
            p_message => pkg_esign_http.msg_service_token
        );
        RETURN;
    END IF;

    pkg_esign_client_api.pr_upsert_env(
        p_client_id => TO_NUMBER(json_value(l_body, '$.client_id')),
        p_role      => NVL(json_value(l_body, '$.role'), 'owner'),
        p_body      => l_body,
        p_out       => l_out
    );
    COMMIT;
    htp.p(l_out);
END;]');

  ---------------------------------------------------------------------------
  -- EVENTS: registrar evento (cancelacion/inutilizacion).
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign_internal', p_pattern => 'events');
  ords.define_handler(
    p_module_name => 'esign_internal', p_pattern => 'events', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    IF NOT pkg_esign_util.fn_service_token_ok(owa_util.get_cgi_env('X-Service-Token')) THEN
        pkg_esign_http.pr_error(
            p_status  => pkg_esign_http.c_unauthorized,
            p_reason  => pkg_esign_http.m_unauthorized,
            p_code    => pkg_esign_http.e_unauthorized,
            p_message => pkg_esign_http.msg_service_token
        );
        RETURN;
    END IF;

    pkg_esign_document_api.pr_register_event(
        p_client_id => TO_NUMBER(json_value(l_body, '$.client_id')),
        p_body      => l_body,
        p_out       => l_out
    );
    COMMIT;
    htp.p(l_out);
END;]');

  ---------------------------------------------------------------------------
  -- LOGS: bitacora de la API.
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign_internal', p_pattern => 'logs');
  ords.define_handler(
    p_module_name => 'esign_internal', p_pattern => 'logs', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
BEGIN
    IF NOT pkg_esign_util.fn_service_token_ok(owa_util.get_cgi_env('X-Service-Token')) THEN
        pkg_esign_http.pr_error(
            p_status  => pkg_esign_http.c_unauthorized,
            p_reason  => pkg_esign_http.m_unauthorized,
            p_code    => pkg_esign_http.e_unauthorized,
            p_message => pkg_esign_http.msg_service_token
        );
        RETURN;
    END IF;

    -- El log puede no tener tenant (fallo pre-auth): bypass acotado de la VPD para insertar.
    pkg_esign_session.begin_bootstrap;
    INSERT INTO api_log (client_id, environment, endpoint, http_status, latency_ms)
    VALUES (TO_NUMBER(json_value(l_body, '$.client_id')), json_value(l_body, '$.environment'),
            json_value(l_body, '$.endpoint'), TO_NUMBER(json_value(l_body, '$.http_status')),
            TO_NUMBER(json_value(l_body, '$.latency_ms')));
    pkg_esign_session.end_bootstrap;
    COMMIT;
    htp.p(pkg_esign_util.fn_ok());
END;]');

  COMMIT;
END;
/
