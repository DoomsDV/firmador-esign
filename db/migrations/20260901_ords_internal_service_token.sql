-- Corrige la autenticacion del modulo interno sin modificar datos de negocio.
-- ORDS entrega headers custom declarados como parametros, no de forma fiable por
-- OWA_UTIL.GET_CGI_ENV. Tambien se usa :status_code para estados HTTP reales.
DECLARE
  l_source CLOB;
  l_count  PLS_INTEGER := 0;
BEGIN
  FOR r IN (
    SELECT t.uri_template,
           h.method,
           h.source,
           h.source_type,
           h.items_per_page,
           h.mimes_allowed,
           h.comments
      FROM user_ords_modules m
      JOIN user_ords_templates t ON t.module_id = m.id
      JOIN user_ords_handlers h ON h.template_id = t.id
     WHERE m.name = 'esign_internal'
  ) LOOP
    l_source := REPLACE(
      r.source,
      'pkg_esign_util.fn_service_token_ok(owa_util.get_cgi_env(''X-Service-Token''))',
      'pkg_esign_util.fn_service_token_ok(:service_token)'
    );
    IF INSTR(l_source, ':status_code := pkg_esign_http.c_unauthorized;') = 0 THEN
      l_source := REPLACE(
        l_source,
        'IF NOT pkg_esign_util.fn_service_token_ok(:service_token) THEN' || CHR(10) ||
        '        pkg_esign_http.pr_error(',
        'IF NOT pkg_esign_util.fn_service_token_ok(:service_token) THEN' || CHR(10) ||
        '        :status_code := pkg_esign_http.c_unauthorized;' || CHR(10) ||
        '        pkg_esign_http.pr_error('
      );
    END IF;

    IF r.uri_template = 'context' THEN
      IF INSTR(l_source, ':status_code := pkg_esign_http.c_forbidden;') = 0 THEN
        l_source := REPLACE(
          l_source,
          'IF l_status <> ''ACTIVE'' THEN' || CHR(10) ||
          '        pkg_esign_http.pr_error(',
          'IF l_status <> ''ACTIVE'' THEN' || CHR(10) ||
          '        :status_code := pkg_esign_http.c_forbidden;' || CHR(10) ||
          '        pkg_esign_http.pr_error('
        );
      END IF;
    ELSIF r.uri_template = 'documents/xml' THEN
      IF INSTR(l_source, ':status_code := pkg_esign_http.c_not_found;') = 0 THEN
        l_source := REPLACE(
          l_source,
          'IF SQLCODE = pkg_esign_http.c_ora_not_found THEN' || CHR(10) ||
          '                pkg_esign_http.pr_error(',
          'IF SQLCODE = pkg_esign_http.c_ora_not_found THEN' || CHR(10) ||
          '                :status_code := pkg_esign_http.c_not_found;' || CHR(10) ||
          '                pkg_esign_http.pr_error('
        );
      END IF;
      IF INSTR(l_source, ':status_code := pkg_esign_http.c_conflict;') = 0 THEN
        l_source := REPLACE(
          l_source,
          'ELSIF SQLCODE = pkg_esign_http.c_ora_conflict THEN' || CHR(10) ||
          '                pkg_esign_http.pr_error(',
          'ELSIF SQLCODE = pkg_esign_http.c_ora_conflict THEN' || CHR(10) ||
          '                :status_code := pkg_esign_http.c_conflict;' || CHR(10) ||
          '                pkg_esign_http.pr_error('
        );
      END IF;
    END IF;

    ords.define_handler(
      p_module_name    => 'esign_internal',
      p_pattern        => r.uri_template,
      p_method         => r.method,
      p_source_type    => r.source_type,
      p_items_per_page => r.items_per_page,
      p_mimes_allowed  => r.mimes_allowed,
      p_comments       => r.comments,
      p_source         => l_source
    );
    ords.define_parameter(
      p_module_name        => 'esign_internal',
      p_pattern            => r.uri_template,
      p_method             => r.method,
      p_name               => 'X-Service-Token',
      p_bind_variable_name => 'service_token',
      p_source_type        => 'HEADER',
      p_param_type         => 'STRING',
      p_access_method      => 'IN',
      p_comments           => 'Token compartido Go<->ORDS'
    );
    l_count := l_count + 1;
  END LOOP;

  IF l_count = 0 THEN
    raise_application_error(-20000, 'modulo ORDS esign_internal no encontrado');
  END IF;

  COMMIT;
END;
/
