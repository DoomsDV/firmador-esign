-- Migracion: preferencia mostrar_fantasia en client_kude_config (toggle del encabezado KuDE).
-- Idempotente. Deshabilita VPD durante ALTER (ORA-28133).
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
    dbms_rls.enable_policy(l_schema, 'CLIENT_KUDE_CONFIG', 'VPD_CLIENT_KUDE_CONFIG', FALSE);
  EXCEPTION WHEN OTHERS THEN NULL;
  END;

  BEGIN
    add_col('ALTER TABLE client_kude_config ADD (mostrar_fantasia NUMBER(1) DEFAULT 1 NOT NULL)');
  EXCEPTION
    WHEN OTHERS THEN
      BEGIN dbms_rls.enable_policy(l_schema, 'CLIENT_KUDE_CONFIG', 'VPD_CLIENT_KUDE_CONFIG', TRUE); EXCEPTION WHEN OTHERS THEN NULL; END;
      RAISE;
  END;

  BEGIN
    dbms_rls.enable_policy(l_schema, 'CLIENT_KUDE_CONFIG', 'VPD_CLIENT_KUDE_CONFIG', TRUE);
  EXCEPTION WHEN OTHERS THEN NULL;
  END;
END;
