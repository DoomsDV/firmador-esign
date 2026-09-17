-- Data Pump / import a aoxdevelop dejó:
--   * el application context ESIGN_CTX sin crear (SET_CONTEXT -> ORA-01031)
--   * las PK identity como NUMBER NOT NULL sin secuencia
--   * los DEFAULT de flags/timestamps
--   * las politicas VPD
-- Oracle no convierte una columna existente a IDENTITY (ORA-30673), así que
-- el equivalente es SEQUENCE + DEFAULT ON NULL seq.NEXTVAL (START WITH = MAX+1).
-- Aplicar las identity/defaults ANTES de reponer VPD: ALTER TABLE con policy
-- activa puede fallar (ORA-28133).

PROMPT === 20260911_restore_identity_context ===

BEGIN
    EXECUTE IMMEDIATE 'CREATE OR REPLACE CONTEXT esign_ctx USING pkg_esign_session';
END;
/

DECLARE
    v_ok_seq   PLS_INTEGER := 0;
    v_ok_def   PLS_INTEGER := 0;
    v_skip     PLS_INTEGER := 0;
    v_fail     PLS_INTEGER := 0;
    v_errors   CLOB := EMPTY_CLOB();
    v_exists   PLS_INTEGER;
    v_next     NUMBER;

    PROCEDURE note_fail(pi_what VARCHAR2) IS
    BEGIN
        v_fail := v_fail + 1;
        v_errors := v_errors || pi_what || CHR(10);
    END;

    PROCEDURE restore_seq(pi_table VARCHAR2, pi_column VARCHAR2, pi_seq VARCHAR2) IS
    BEGIN
        SELECT COUNT(*) INTO v_exists FROM user_tables WHERE table_name = UPPER(pi_table);
        IF v_exists = 0 THEN v_skip := v_skip + 1; RETURN; END IF;

        SELECT COUNT(*) INTO v_exists FROM user_sequences WHERE sequence_name = UPPER(pi_seq);
        IF v_exists = 0 THEN
            EXECUTE IMMEDIATE 'SELECT NVL(MAX(' || pi_column || '),0)+1 FROM ' || pi_table INTO v_next;
            EXECUTE IMMEDIATE 'CREATE SEQUENCE ' || pi_seq || ' START WITH ' || v_next || ' CACHE 20';
        END IF;

        EXECUTE IMMEDIATE 'ALTER TABLE ' || pi_table || ' MODIFY (' || pi_column
            || ' DEFAULT ON NULL ' || pi_seq || '.NEXTVAL)';
        v_ok_seq := v_ok_seq + 1;
    EXCEPTION
        WHEN OTHERS THEN note_fail(pi_table || '.' || pi_column || ' SEQ: ' || SQLERRM);
    END;

    PROCEDURE restore_def(pi_table VARCHAR2, pi_column VARCHAR2, pi_default VARCHAR2) IS
    BEGIN
        SELECT COUNT(*) INTO v_exists FROM user_tab_columns
         WHERE table_name = UPPER(pi_table) AND column_name = UPPER(pi_column);
        IF v_exists = 0 THEN v_skip := v_skip + 1; RETURN; END IF;
        EXECUTE IMMEDIATE 'ALTER TABLE ' || pi_table || ' MODIFY (' || pi_column
            || ' DEFAULT ' || pi_default || ')';
        v_ok_def := v_ok_def + 1;
    EXCEPTION
        WHEN OTHERS THEN note_fail(pi_table || '.' || pi_column || ' DEF: ' || SQLERRM);
    END;
BEGIN
    restore_seq('users', 'id_user', 'seq_users');
    restore_seq('client', 'id_client', 'seq_client');
    restore_seq('client_user', 'id_client_user', 'seq_client_user');
    restore_seq('user_session', 'id_user_session', 'seq_user_session');
    restore_seq('client_emisor', 'id_client_emisor', 'seq_client_emisor');
    restore_seq('client_emisor_actividad', 'id_actividad', 'seq_client_emisor_actividad');
    restore_seq('client_establecimiento', 'id_establecimiento', 'seq_client_establecimiento');
    restore_seq('client_punto_expedicion', 'id_punto', 'seq_client_punto_expedicion');
    restore_seq('client_sifen_env', 'id_sifen_env', 'seq_client_sifen_env');
    restore_seq('document_sequence', 'id_document_sequence', 'seq_document_sequence');
    restore_seq('document', 'id_document', 'seq_document');
    restore_seq('document_event', 'id_document_event', 'seq_document_event');
    restore_seq('api_log', 'id_api_log', 'seq_api_log');
    restore_seq('client_api_key', 'id_client_api_key', 'seq_client_api_key');
    restore_seq('client_certificate', 'id_client_certificate', 'seq_client_certificate');
    restore_seq('client_invitation', 'id_client_invitation', 'seq_client_invitation');
    restore_seq('document_kude_task', 'id_task', 'seq_document_kude_task');
    restore_seq('document_webhook_delivery', 'id_delivery', 'seq_document_webhook_delivery');

    restore_def('app_parameter', 'updated_at', 'CURRENT_TIMESTAMP');
    restore_def('users', 'is_active', '0');
    restore_def('users', 'created_at', 'CURRENT_TIMESTAMP');
    restore_def('client', 'status', '''ACTIVE''');
    restore_def('client', 'cert_key_version', '1');
    restore_def('client', 'created_at', 'CURRENT_TIMESTAMP');
    restore_def('client_user', 'is_active', '1');
    restore_def('client_user', 'created_at', 'CURRENT_TIMESTAMP');
    restore_def('user_session', 'is_revoked', '0');
    restore_def('user_session', 'created_at', 'CURRENT_TIMESTAMP');
    restore_def('client_establecimiento', 'num_casa', '''0''');
    restore_def('client_establecimiento', 'is_active', '1');
    restore_def('client_emisor_actividad', 'is_active', '1');
    restore_def('client_punto_expedicion', 'is_active', '1');
    restore_def('client_sifen_env', 'key_version', '1');
    restore_def('client_sifen_env', 'is_active', '1');
    restore_def('document_sequence', 'last_number', '0');
    restore_def('document', 'retry_requested', '0');
    restore_def('document', 'retry_count', '0');
    restore_def('document', 'recovery_required', '0');
    restore_def('document', 'created_at', 'CURRENT_TIMESTAMP');
    restore_def('document_xml', 'xml_mime_type', '''application/xml; charset=UTF-8''');
    restore_def('document_xml', 'xml_availability', '''MISSING''');
    restore_def('document_event', 'created_at', 'CURRENT_TIMESTAMP');
    restore_def('api_log', 'created_at', 'CURRENT_TIMESTAMP');
    restore_def('client_api_key', 'status', '''ACTIVE''');
    restore_def('client_api_key', 'created_at', 'CURRENT_TIMESTAMP');
    restore_def('client_certificate', 'key_version', '1');
    restore_def('client_certificate', 'status', '''ACTIVE''');
    restore_def('client_certificate', 'created_at', 'CURRENT_TIMESTAMP');
    restore_def('client_invitation', 'status', '''PENDING''');
    restore_def('client_invitation', 'created_at', 'CURRENT_TIMESTAMP');
    restore_def('client_kude_config', 'template_id', '''minimalista''');
    restore_def('client_kude_config', 'color_primario', '''#0f172a''');
    restore_def('client_kude_config', 'mostrar_fantasia', '1');
    restore_def('client_kude_config', 'updated_at', 'CURRENT_TIMESTAMP');
    restore_def('document_idempotency', 'phase', '''CLAIMED''');
    restore_def('document_idempotency', 'created_at', 'CURRENT_TIMESTAMP');
    restore_def('document_idempotency', 'updated_at', 'CURRENT_TIMESTAMP');
    restore_def('document_kude_task', 'attempts', '0');
    restore_def('document_kude_task', 'max_attempts', '10');
    restore_def('document_kude_task', 'created_at', 'CURRENT_TIMESTAMP');
    restore_def('document_kude_task', 'updated_at', 'CURRENT_TIMESTAMP');
    restore_def('client_webhook_endpoint', 'key_version', '1');
    restore_def('client_webhook_endpoint', 'is_active', '0');
    restore_def('client_webhook_endpoint', 'updated_at', 'CURRENT_TIMESTAMP');
    restore_def('document_webhook_delivery', 'attempts', '0');
    restore_def('document_webhook_delivery', 'max_attempts', '10');
    restore_def('document_webhook_delivery', 'created_at', 'CURRENT_TIMESTAMP');
    restore_def('document_webhook_delivery', 'updated_at', 'CURRENT_TIMESTAMP');

    DBMS_OUTPUT.PUT_LINE('seq=' || v_ok_seq || ' def=' || v_ok_def || ' skip=' || v_skip || ' fail=' || v_fail);
    IF v_fail > 0 THEN
        raise_application_error(-20000, 'restore fail=' || v_fail || CHR(10) || DBMS_LOB.SUBSTR(v_errors, 2000, 1));
    END IF;
END;
/

-- VPD (perdida en el import). Idempotente.
DECLARE
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
        'CLIENT_KUDE_CONFIG',
        'CLIENT_WEBHOOK_ENDPOINT',
        'DOCUMENT_WEBHOOK_DELIVERY'
    );

    PROCEDURE add_policy(p_table VARCHAR2, p_func VARCHAR2) IS
    BEGIN
        BEGIN
            dbms_rls.drop_policy(
                object_schema => sys_context('userenv','current_schema'),
                object_name   => p_table,
                policy_name   => 'VPD_' || p_table
            );
        EXCEPTION WHEN OTHERS THEN NULL;
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
    add_policy('CLIENT', 'fn_client_policy');
END;
/
