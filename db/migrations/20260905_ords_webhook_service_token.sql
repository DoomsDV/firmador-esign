-- Binding X-Service-Token en handlers internos webhook (store-secret, delivery, etc.).
-- Sin esto ORDS no liga el header y fn_service_token_ok devuelve 401 siempre.
BEGIN
  FOR r IN (
    SELECT t.uri_template, h.method
      FROM user_ords_modules m
      JOIN user_ords_templates t ON t.module_id = m.id
      JOIN user_ords_handlers h ON h.template_id = t.id
     WHERE m.name = 'esign_internal'
       AND (
             t.uri_template LIKE 'webhook/%'
          OR t.uri_template LIKE 'webhook-delivery/%'
       )
  ) LOOP
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
  END LOOP;
  COMMIT;
END;
/
