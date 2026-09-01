-- Migracion: motor de KuDE con Gotenberg + OCI. Agrega document_xml.kude_url (la
-- columna kude_blob queda sin uso, se puede eliminar en una migracion futura) y
-- crea client_kude_config (branding por cliente). Idempotente.
-- La VPD sobre document_xml bloquea ALTER ... ADD con ORA-28133 si la politica esta
-- activa: se deshabilita durante el ALTER y se re-habilita siempre (incluso ante error).
DECLARE
  l_schema VARCHAR2(128) := sys_context('userenv', 'current_schema');
  PROCEDURE add_col(p_ddl VARCHAR2) IS
  BEGIN
    EXECUTE IMMEDIATE p_ddl;
  EXCEPTION
    WHEN OTHERS THEN
      IF SQLCODE = -1430 THEN NULL; -- ORA-01430: column already exists
      ELSE RAISE; END IF;
  END;
BEGIN
  BEGIN
    dbms_rls.enable_policy(l_schema, 'DOCUMENT_XML', 'VPD_DOCUMENT_XML', FALSE);
  EXCEPTION WHEN OTHERS THEN NULL;
  END;

  BEGIN
    add_col('ALTER TABLE document_xml ADD (kude_url VARCHAR2(1000))');
  EXCEPTION
    WHEN OTHERS THEN
      BEGIN dbms_rls.enable_policy(l_schema, 'DOCUMENT_XML', 'VPD_DOCUMENT_XML', TRUE); EXCEPTION WHEN OTHERS THEN NULL; END;
      RAISE;
  END;

  BEGIN
    dbms_rls.enable_policy(l_schema, 'DOCUMENT_XML', 'VPD_DOCUMENT_XML', TRUE);
  EXCEPTION WHEN OTHERS THEN NULL;
  END;
END;
/

-- client_kude_config no existe todavia en instalaciones anteriores a esta migracion.
BEGIN
  EXECUTE IMMEDIATE q'[
    CREATE TABLE client_kude_config (
      client_id      NUMBER PRIMARY KEY,
      template_id    VARCHAR2(30)  DEFAULT 'minimalista' NOT NULL,
      color_primario VARCHAR2(7)   DEFAULT '#0f172a' NOT NULL,
      logo_url       VARCHAR2(500),
      notas_footer   VARCHAR2(500),
      updated_at     TIMESTAMP(6) WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP NOT NULL,
      CONSTRAINT fk_kude_config_client FOREIGN KEY (client_id) REFERENCES client (id_client) ON DELETE CASCADE,
      CONSTRAINT ck_kude_template CHECK (template_id IN ('minimalista', 'corporativa'))
    )]';
EXCEPTION
  WHEN OTHERS THEN
    IF SQLCODE = -955 THEN NULL; -- ORA-00955: object already exists
    ELSE RAISE; END IF;
END;
/

COMMENT ON TABLE client_kude_config IS 'Branding del KuDE por cliente: plantilla, color primario, logo y notas de pie.';

-- VPD: client_kude_config se filtra por client_id como el resto de tablas del tenant.
BEGIN
  BEGIN
    dbms_rls.drop_policy(
      object_schema => sys_context('userenv','current_schema'),
      object_name   => 'CLIENT_KUDE_CONFIG',
      policy_name   => 'VPD_CLIENT_KUDE_CONFIG'
    );
  EXCEPTION WHEN OTHERS THEN NULL;
  END;

  dbms_rls.add_policy(
    object_schema   => sys_context('userenv','current_schema'),
    object_name     => 'CLIENT_KUDE_CONFIG',
    policy_name     => 'VPD_CLIENT_KUDE_CONFIG',
    function_schema => sys_context('userenv','current_schema'),
    policy_function => 'fn_tenant_policy',
    statement_types => 'SELECT,INSERT,UPDATE,DELETE',
    update_check    => TRUE
  );
END;
/
