-- app_parameter: parametros globales de la app (llaves JWT, tokens de servicio, etc.).
-- No lleva client_id: es configuracion global, fuera de la VPD por tenant.
CREATE TABLE app_parameter (
  param_key    VARCHAR2(100) PRIMARY KEY,
  param_value  VARCHAR2(4000),
  description  VARCHAR2(400),
  updated_at   TIMESTAMP(6) WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP NOT NULL
);

COMMENT ON TABLE  app_parameter IS 'Parametros globales: JWT_TOKEN, JWT_ISSUER, JWT_AUDIENCE, JWT_ACCESS_EXP_SEC, JWT_REFRESH_EXP_DAYS, SERVICE_TOKEN (Go<->ORDS).';
COMMENT ON COLUMN app_parameter.param_key   IS 'Clave del parametro.';
COMMENT ON COLUMN app_parameter.param_value IS 'Valor del parametro (secretos incluidos; acceso restringido por grants).';
