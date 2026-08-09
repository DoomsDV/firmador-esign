-- client_kude_config: branding del KuDE (1:1 por client). Plantilla, color primario y
-- logo se usan para renderizar el HTML (internal/kude) que luego convierte Gotenberg.
CREATE TABLE client_kude_config (
  client_id      NUMBER PRIMARY KEY,
  template_id    VARCHAR2(30)  DEFAULT 'minimalista' NOT NULL,  -- 'minimalista' | 'corporativa'
  color_primario VARCHAR2(7)   DEFAULT '#0f172a' NOT NULL,      -- HEX, ej. #0f172a
  logo_url       VARCHAR2(500),                                 -- URL publica en bucket-etick-dev
  notas_footer   VARCHAR2(500),
  updated_at     TIMESTAMP(6) WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP NOT NULL,
  CONSTRAINT fk_kude_config_client FOREIGN KEY (client_id) REFERENCES client (id_client) ON DELETE CASCADE,
  CONSTRAINT ck_kude_template CHECK (template_id IN ('minimalista', 'corporativa'))
);

COMMENT ON TABLE client_kude_config IS 'Branding del KuDE por cliente: plantilla, color primario, logo y notas de pie.';
