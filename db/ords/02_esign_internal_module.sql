-- Modulo ORDS 'esign_internal' (/internal/v1/) para el servicio Go. Cada handler valida el
-- header X-Service-Token contra app_parameter.SERVICE_TOKEN antes de tocar los paquetes. La
-- API key del comercio (en el body) resuelve el client_id + environment (pr_resolve_key).
-- Nota: el cuerpo se lee UNA sola vez en l_body := :body_text (CLOB); :body es BLOB y no liga
-- con json_value/CLOB, y referenciar :body_text mas de una vez en el mismo handler falla.
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
  l_body CLOB := :body_text;
  l_out CLOB; l_cid NUMBER; l_env VARCHAR2(4); l_status VARCHAR2(20);
BEGIN
  IF NOT pkg_esign_util.fn_service_token_ok(owa_util.get_cgi_env('X-Service-Token')) THEN
    owa_util.status_line(401, 'Unauthorized'); htp.p('{"success":false,"error":{"code":"UNAUTHORIZED","message":"service token invalido"}}'); RETURN;
  END IF;
  pkg_esign_apikey_api.pr_resolve_key(json_value(l_body, '$.api_key'), l_cid, l_env, l_status);
  IF l_status <> 'ACTIVE' THEN
    owa_util.status_line(403, 'Forbidden'); htp.p('{"success":false,"error":{"code":"KEY_REVOKED","message":"API key no activa"}}'); RETURN;
  END IF;
  pkg_esign_client_api.pr_get_internal_context(l_cid, l_env, l_out);
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
DECLARE l_body CLOB := :body_text; l_num NUMBER; l_data CLOB; BEGIN
  IF NOT pkg_esign_util.fn_service_token_ok(owa_util.get_cgi_env('X-Service-Token')) THEN
    owa_util.status_line(401, 'Unauthorized'); htp.p('{"success":false}'); RETURN;
  END IF;
  pkg_esign_client_api.pr_next_document_number(
    TO_NUMBER(json_value(l_body, '$.client_id')),
    json_value(l_body, '$.environment'),
    json_value(l_body, '$.establecimiento'),
    json_value(l_body, '$.punto_expedicion'),
    TO_NUMBER(json_value(l_body, '$.tipo_de')), l_num);
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
DECLARE l_body CLOB := :body_text; l_out CLOB; BEGIN
  IF NOT pkg_esign_util.fn_service_token_ok(owa_util.get_cgi_env('X-Service-Token')) THEN
    owa_util.status_line(401, 'Unauthorized'); htp.p('{"success":false}'); RETURN;
  END IF;
  pkg_esign_cert_api.pr_get_certificate_json(TO_NUMBER(json_value(l_body, '$.client_id')), l_out);
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
DECLARE l_body CLOB := :body_text; l_out CLOB; BEGIN
  IF NOT pkg_esign_util.fn_service_token_ok(owa_util.get_cgi_env('X-Service-Token')) THEN
    owa_util.status_line(401, 'Unauthorized'); htp.p('{"success":false}'); RETURN;
  END IF;
  pkg_esign_client_api.pr_get_csc_json(TO_NUMBER(json_value(l_body, '$.client_id')),
                                       json_value(l_body, '$.environment'), l_out);
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
DECLARE l_body CLOB := :body_text; l_out CLOB; BEGIN
  IF NOT pkg_esign_util.fn_service_token_ok(owa_util.get_cgi_env('X-Service-Token')) THEN
    owa_util.status_line(401, 'Unauthorized'); htp.p('{"success":false}'); RETURN;
  END IF;
  pkg_esign_document_api.pr_register_document(TO_NUMBER(json_value(l_body, '$.client_id')), l_body, l_out);
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
DECLARE l_body CLOB := :body_text; l_out CLOB; BEGIN
  IF NOT pkg_esign_util.fn_service_token_ok(owa_util.get_cgi_env('X-Service-Token')) THEN
    owa_util.status_line(401, 'Unauthorized'); htp.p('{"success":false}'); RETURN;
  END IF;
  pkg_esign_document_api.pr_register_event(TO_NUMBER(json_value(l_body, '$.client_id')), l_body, l_out);
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
DECLARE l_body CLOB := :body_text; BEGIN
  IF NOT pkg_esign_util.fn_service_token_ok(owa_util.get_cgi_env('X-Service-Token')) THEN
    owa_util.status_line(401, 'Unauthorized'); htp.p('{"success":false}'); RETURN;
  END IF;
  -- El log puede no tener tenant (fallo pre-auth): bypass acotado de la VPD para insertar.
  pkg_esign_session.begin_bootstrap;
  INSERT INTO api_log (client_id, environment, endpoint, http_status, latency_ms)
  VALUES (TO_NUMBER(json_value(l_body, '$.client_id')), json_value(l_body, '$.environment'),
          json_value(l_body, '$.endpoint'), TO_NUMBER(json_value(l_body, '$.http_status')),
          TO_NUMBER(json_value(l_body, '$.latency_ms')));
  pkg_esign_session.end_bootstrap;
  COMMIT;
  htp.p('{"success":true}');
END;]');

  COMMIT;
END;
/
