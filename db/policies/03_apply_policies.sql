-- Aplica VPD (DBMS_RLS) sobre todas las tablas del tenant. Idempotente: elimina la politica
-- si ya existe antes de recrearla. update_check => TRUE impide insertar/actualizar filas de
-- otro client. statement_types cubre SELECT/INSERT/UPDATE/DELETE.
DECLARE
  -- Tablas con columna client_id -> fn_tenant_policy
  TYPE t_tab IS TABLE OF VARCHAR2(30);
  l_tenant_tables t_tab := t_tab(
    'CLIENT_USER',
    'CLIENT_EMISOR',
    'CLIENT_EMISOR_ACTIVIDAD',
    'CLIENT_ESTABLECIMIENTO',
    'CLIENT_PUNTO_EXPEDICION',
    'CLIENT_SIFEN_ENV',
    'CLIENT_API_KEY',
    'CLIENT_CERTIFICATE',
    'CLIENT_INVITATION',
    'DOCUMENT_SEQUENCE',
    'DOCUMENT',
    'DOCUMENT_IDEMPOTENCY',
    'DOCUMENT_XML',
    'DOCUMENT_EVENT',
    'DOCUMENT_KUDE_TASK',
    'API_LOG',
    'CLIENT_KUDE_CONFIG'
  );

  PROCEDURE add_policy(p_table VARCHAR2, p_func VARCHAR2) IS
  BEGIN
    BEGIN
      dbms_rls.drop_policy(
        object_schema => sys_context('userenv','current_schema'),
        object_name   => p_table,
        policy_name   => 'VPD_' || p_table
      );
    EXCEPTION WHEN OTHERS THEN NULL; -- no existia
    END;

    dbms_rls.add_policy(
      object_schema   => sys_context('userenv','current_schema'),
      object_name     => p_table,
      policy_name     => 'VPD_' || p_table,
      function_schema => sys_context('userenv','current_schema'),
      policy_function => p_func,
      statement_types => 'SELECT,INSERT,UPDATE,DELETE',
      update_check    => TRUE
    );
  END add_policy;
BEGIN
  FOR i IN 1 .. l_tenant_tables.COUNT LOOP
    add_policy(l_tenant_tables(i), 'fn_tenant_policy');
  END LOOP;

  -- La tabla client se filtra por su PK id_client (no lleva columna client_id).
  add_policy('CLIENT', 'fn_client_policy');
END;
/
