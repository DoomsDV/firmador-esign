-- ORDS: webhooks panel (/api/v1/webhooks/:env) e interno (cola + secret).
BEGIN
  ---------------------------------------------------------------------------
  -- Panel: GET/PUT /webhooks/:env
  ---------------------------------------------------------------------------
  ORDS.define_template(p_module_name => 'esign', p_pattern => 'webhooks/:env');
  ORDS.define_handler(
    p_module_name => 'esign', p_pattern => 'webhooks/:env', p_method => 'GET',
    p_source_type => ORDS.source_type_plsql,
    p_source => q'[
DECLARE
    l_out  CLOB;
    l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
BEGIN
    pkg_esign_webhook_api.pr_get(
        p_client_id   => pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
        p_role        => pkg_esign_util.fn_get_role_from_jwt(l_auth),
        p_environment => UPPER(:env),
        p_out         => l_out
    );
    htp.p(l_out);
END;]');

  ORDS.define_handler(
    p_module_name => 'esign', p_pattern => 'webhooks/:env', p_method => 'PUT',
    p_source_type => ORDS.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
    l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
BEGIN
    pkg_esign_webhook_api.pr_upsert(
        p_client_id   => pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
        p_role        => pkg_esign_util.fn_get_role_from_jwt(l_auth),
        p_environment => UPPER(:env),
        p_body        => l_body,
        p_out         => l_out
    );
    COMMIT;
    htp.p(l_out);
END;]');

  ---------------------------------------------------------------------------
  -- Interno: webhook secret, enqueue, claim, complete, recovery
  ---------------------------------------------------------------------------
  ORDS.define_template(p_module_name => 'esign_internal', p_pattern => 'webhook/secret');
  ORDS.define_handler(
    p_module_name => 'esign_internal', p_pattern => 'webhook/secret', p_method => 'POST',
    p_source_type => ORDS.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    IF NOT pkg_esign_util.fn_service_token_ok(:service_token) THEN
        :status_code := pkg_esign_http.c_unauthorized;
        pkg_esign_http.pr_error(
            p_status  => pkg_esign_http.c_unauthorized,
            p_reason  => pkg_esign_http.m_unauthorized,
            p_code    => pkg_esign_http.e_unauthorized,
            p_message => pkg_esign_http.msg_service_token
        );
        RETURN;
    END IF;

    pkg_esign_webhook_api.pr_get_secret_json(
        p_client_id   => TO_NUMBER(json_value(l_body, '$.client_id')),
        p_environment => json_value(l_body, '$.environment'),
        p_out         => l_out
    );
    htp.p(l_out);
END;]');

  ORDS.define_template(p_module_name => 'esign_internal', p_pattern => 'webhook/store-secret');
  ORDS.define_handler(
    p_module_name => 'esign_internal', p_pattern => 'webhook/store-secret', p_method => 'POST',
    p_source_type => ORDS.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    IF NOT pkg_esign_util.fn_service_token_ok(:service_token) THEN
        :status_code := pkg_esign_http.c_unauthorized;
        pkg_esign_http.pr_error(
            p_status  => pkg_esign_http.c_unauthorized,
            p_reason  => pkg_esign_http.m_unauthorized,
            p_code    => pkg_esign_http.e_unauthorized,
            p_message => pkg_esign_http.msg_service_token
        );
        RETURN;
    END IF;

    pkg_esign_webhook_api.pr_store_secret(
        p_client_id   => TO_NUMBER(json_value(l_body, '$.client_id')),
        p_environment => json_value(l_body, '$.environment'),
        p_body        => l_body,
        p_out         => l_out
    );
    COMMIT;
    htp.p(l_out);
END;]');

  ORDS.define_template(p_module_name => 'esign_internal', p_pattern => 'webhook-delivery/enqueue');
  ORDS.define_handler(
    p_module_name => 'esign_internal', p_pattern => 'webhook-delivery/enqueue', p_method => 'POST',
    p_source_type => ORDS.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    IF NOT pkg_esign_util.fn_service_token_ok(:service_token) THEN
        :status_code := pkg_esign_http.c_unauthorized;
        pkg_esign_http.pr_error(
            p_status  => pkg_esign_http.c_unauthorized,
            p_reason  => pkg_esign_http.m_unauthorized,
            p_code    => pkg_esign_http.e_unauthorized,
            p_message => pkg_esign_http.msg_service_token
        );
        RETURN;
    END IF;

    pkg_esign_webhook_delivery_api.pr_enqueue(
        p_client_id => TO_NUMBER(json_value(l_body, '$.client_id')),
        p_cdc       => json_value(l_body, '$.cdc'),
        p_out       => l_out
    );
    COMMIT;
    htp.p(l_out);
END;]');

  ORDS.define_template(p_module_name => 'esign_internal', p_pattern => 'webhook-delivery/claim');
  ORDS.define_handler(
    p_module_name => 'esign_internal', p_pattern => 'webhook-delivery/claim', p_method => 'POST',
    p_source_type => ORDS.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    IF NOT pkg_esign_util.fn_service_token_ok(:service_token) THEN
        :status_code := pkg_esign_http.c_unauthorized;
        pkg_esign_http.pr_error(
            p_status  => pkg_esign_http.c_unauthorized,
            p_reason  => pkg_esign_http.m_unauthorized,
            p_code    => pkg_esign_http.e_unauthorized,
            p_message => pkg_esign_http.msg_service_token
        );
        RETURN;
    END IF;

    pkg_esign_webhook_delivery_api.pr_claim(
        p_lease_owner   => json_value(l_body, '$.lease_owner'),
        p_lease_seconds => TO_NUMBER(json_value(l_body, '$.lease_seconds')),
        p_limit         => TO_NUMBER(json_value(l_body, '$.limit')),
        p_out           => l_out
    );
    COMMIT;
    htp.p(l_out);
END;]');

  ORDS.define_template(p_module_name => 'esign_internal', p_pattern => 'webhook-delivery/complete');
  ORDS.define_handler(
    p_module_name => 'esign_internal', p_pattern => 'webhook-delivery/complete', p_method => 'POST',
    p_source_type => ORDS.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    IF NOT pkg_esign_util.fn_service_token_ok(:service_token) THEN
        :status_code := pkg_esign_http.c_unauthorized;
        pkg_esign_http.pr_error(
            p_status  => pkg_esign_http.c_unauthorized,
            p_reason  => pkg_esign_http.m_unauthorized,
            p_code    => pkg_esign_http.e_unauthorized,
            p_message => pkg_esign_http.msg_service_token
        );
        RETURN;
    END IF;

    pkg_esign_webhook_delivery_api.pr_complete(
        p_delivery_id => TO_NUMBER(json_value(l_body, '$.delivery_id')),
        p_body        => l_body,
        p_out         => l_out
    );
    COMMIT;
    htp.p(l_out);
END;]');

  ORDS.define_template(p_module_name => 'esign_internal', p_pattern => 'webhook-delivery/recovery');
  ORDS.define_handler(
    p_module_name => 'esign_internal', p_pattern => 'webhook-delivery/recovery', p_method => 'POST',
    p_source_type => ORDS.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    IF NOT pkg_esign_util.fn_service_token_ok(:service_token) THEN
        :status_code := pkg_esign_http.c_unauthorized;
        pkg_esign_http.pr_error(
            p_status  => pkg_esign_http.c_unauthorized,
            p_reason  => pkg_esign_http.m_unauthorized,
            p_code    => pkg_esign_http.e_unauthorized,
            p_message => pkg_esign_http.msg_service_token
        );
        RETURN;
    END IF;

    pkg_esign_webhook_delivery_api.pr_list_recovery(
        p_limit => TO_NUMBER(json_value(l_body, '$.limit')),
        p_out   => l_out
    );
    htp.p(l_out);
END;]');

  COMMIT;
END;
/
