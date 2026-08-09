-- Modulo ORDS 'esign' (/api/v1/) para el panel. Handlers delgados: leen Authorization via
-- owa_util.get_cgi_env('AUTHORIZATION'), el body via l_body := :body_text (CLOB) y delegan en
-- los paquetes PKG_ESIGN_*. La salida JSON se emite con htp.p. La autorizacion real (JWT) la
-- valida cada paquete.
-- Convenciones de los handlers:
--   * El cuerpo se lee UNA sola vez en l_body := :body_text (CLOB); :body es BLOB y no liga
--     con json_value/CLOB, y referenciar :body_text mas de una vez en el mismo handler falla.
--   * Las llamadas a los paquetes usan notacion de parametros nombrados (p_x => valor).
--   * Los codigos/mensajes HTTP salen de PKG_ESIGN_HTTP (sin numeros sueltos).
BEGIN
  BEGIN
    ords.delete_module(p_module_name => 'esign');
  EXCEPTION WHEN OTHERS THEN NULL;
  END;

  ords.define_module(
    p_module_name    => 'esign',
    p_base_path      => '/api/v1/',
    p_items_per_page => 25,
    p_status         => 'PUBLISHED',
    p_comments       => 'API del panel esign (auth JWT).');

  ---------------------------------------------------------------------------
  -- AUTH
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign', p_pattern => 'auth/register');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'auth/register', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    pkg_esign_auth_api.pr_register(
        p_body => l_body,
        p_out  => l_out
    );
    htp.p(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'auth/login');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'auth/login', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    pkg_esign_auth_api.pr_login(
        p_body => l_body,
        p_out  => l_out
    );
    htp.p(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'auth/select-client');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'auth/select-client', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    pkg_esign_auth_api.pr_select_client(
        p_user_id   => TO_NUMBER(json_value(l_body, '$.user_id')),
        p_client_id => TO_NUMBER(json_value(l_body, '$.client_id')),
        p_out       => l_out
    );
    htp.p(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'auth/my-clients');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'auth/my-clients', p_method => 'GET',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_out CLOB;
    l_uid NUMBER;
BEGIN
    l_uid := pkg_esign_util.fn_get_user_id_from_jwt(owa_util.get_cgi_env('AUTHORIZATION'));
    pkg_esign_auth_api.pr_list_my_clients(
        p_user_id => l_uid,
        p_out     => l_out
    );
    htp.p(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'auth/refresh');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'auth/refresh', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    pkg_esign_auth_api.pr_refresh(
        p_body => l_body,
        p_out  => l_out
    );
    htp.p(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'auth/logout');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'auth/logout', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    pkg_esign_auth_api.pr_logout(
        p_body => l_body,
        p_out  => l_out
    );
    htp.p(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'auth/me');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'auth/me', p_method => 'GET',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_out CLOB;
BEGIN
    pkg_esign_auth_api.pr_me(
        p_authorization => owa_util.get_cgi_env('AUTHORIZATION'),
        p_out           => l_out
    );
    htp.p(l_out);
END;]');

  ---------------------------------------------------------------------------
  -- CLIENT / EMISOR
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign', p_pattern => 'client');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'client', p_method => 'GET',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_out  CLOB;
    l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
BEGIN
    pkg_esign_client_api.pr_get_client(
        p_client_id => pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
        p_out       => l_out
    );
    htp.p(l_out);
END;]');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'client', p_method => 'PUT',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
    l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
BEGIN
    pkg_esign_client_api.pr_upsert_emisor(
        p_client_id => pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
        p_role      => pkg_esign_util.fn_get_role_from_jwt(l_auth),
        p_body      => l_body,
        p_out       => l_out
    );
    htp.p(l_out);
END;]');

  ---------------------------------------------------------------------------
  -- INVITACIONES (base). POST crea una invitacion PENDING con token + expiracion.
  -- FUTURO: el envio del correo y la aceptacion por token quedan pendientes.
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign', p_pattern => 'invitations');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'invitations', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    pkg_esign_auth_api.pr_invite_user(
        p_authorization => owa_util.get_cgi_env('AUTHORIZATION'),
        p_body          => l_body,
        p_out           => l_out
    );
    htp.p(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'invitations/accept');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'invitations/accept', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
BEGIN
    pkg_esign_auth_api.pr_accept_invitation(
        p_body => l_body,
        p_out  => l_out
    );
    htp.p(l_out);
END;]');

  ---------------------------------------------------------------------------
  -- ESTABLECIMIENTOS / PUNTOS
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign', p_pattern => 'establecimientos');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'establecimientos', p_method => 'GET',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_out  CLOB;
    l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
BEGIN
    pkg_esign_client_api.pr_list_establecimiento(
        p_client_id => pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
        p_out       => l_out
    );
    htp.p(l_out);
END;]');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'establecimientos', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
    l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
BEGIN
    pkg_esign_client_api.pr_upsert_establecimiento(
        p_client_id => pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
        p_role      => pkg_esign_util.fn_get_role_from_jwt(l_auth),
        p_body      => l_body,
        p_out       => l_out
    );
    htp.p(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'establecimientos/:codigo/puntos');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'establecimientos/:codigo/puntos', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
    l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
BEGIN
    pkg_esign_client_api.pr_upsert_punto(
        p_client_id    => pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
        p_role         => pkg_esign_util.fn_get_role_from_jwt(l_auth),
        p_estab_codigo => :codigo,
        p_body         => l_body,
        p_out          => l_out
    );
    htp.p(l_out);
END;]');

  ---------------------------------------------------------------------------
  -- ENVIRONMENTS (timbrado/CSC)
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign', p_pattern => 'environments');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'environments', p_method => 'PUT',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
    l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
BEGIN
    pkg_esign_client_api.pr_upsert_env(
        p_client_id => pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
        p_role      => pkg_esign_util.fn_get_role_from_jwt(l_auth),
        p_body      => l_body,
        p_out       => l_out
    );
    htp.p(l_out);
END;]');

  ---------------------------------------------------------------------------
  -- API KEYS
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign', p_pattern => 'api-keys');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'api-keys', p_method => 'GET',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_out  CLOB;
    l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
BEGIN
    pkg_esign_apikey_api.pr_get_keys_meta(
        p_client_id => pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
        p_out       => l_out
    );
    htp.p(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'api-keys/:env/rotate');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'api-keys/:env/rotate', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_out  CLOB;
    l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
BEGIN
    pkg_esign_apikey_api.pr_rotate_key(
        p_client_id   => pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
        p_role        => pkg_esign_util.fn_get_role_from_jwt(l_auth),
        p_environment => UPPER(:env),
        p_out         => l_out
    );
    htp.p(l_out);
END;]');

  ---------------------------------------------------------------------------
  -- CERTIFICADO (metadata / subida). El BLOB nunca se expone al panel.
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign', p_pattern => 'certificate');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'certificate', p_method => 'GET',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_out  CLOB;
    l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
BEGIN
    pkg_esign_cert_api.pr_get_meta(
        p_client_id => pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
        p_out       => l_out
    );
    htp.p(l_out);
END;]');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'certificate', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
    l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
BEGIN
    pkg_esign_cert_api.pr_put_certificate(
        p_client_id => pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
        p_role      => pkg_esign_util.fn_get_role_from_jwt(l_auth),
        p_body      => l_body,
        p_out       => l_out
    );
    htp.p(l_out);
END;]');

  ---------------------------------------------------------------------------
  -- KUDE-CONFIG (branding: plantilla/color/logo/footer). Logo sube directo al
  -- bucket OCI via PKG_ESIGN_BUCKET; el PDF final lo sube Go (plano interno).
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign', p_pattern => 'kude-config');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'kude-config', p_method => 'GET',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_out  CLOB;
    l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
BEGIN
    pkg_esign_kude_api.pr_get_config(
        p_client_id => pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
        p_out       => l_out
    );
    htp.p(l_out);
END;]');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'kude-config', p_method => 'PUT',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
    l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
BEGIN
    pkg_esign_kude_api.pr_upsert_config(
        p_client_id => pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
        p_role      => pkg_esign_util.fn_get_role_from_jwt(l_auth),
        p_body      => l_body,
        p_out       => l_out
    );
    htp.p(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'kude-config/logo');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'kude-config/logo', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
    l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
BEGIN
    pkg_esign_kude_api.pr_upload_logo(
        p_client_id => pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
        p_role      => pkg_esign_util.fn_get_role_from_jwt(l_auth),
        p_image_hex => json_value(l_body, '$.image_hex' RETURNING CLOB),
        p_mime_type => json_value(l_body, '$.mime_type'),
        p_out       => l_out
    );
    htp.p(l_out);
END;]');

  ---------------------------------------------------------------------------
  -- DOCUMENTOS (lectura)
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign', p_pattern => 'documents');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'documents', p_method => 'GET',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_out  CLOB;
    l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
    l_body CLOB;
BEGIN
    l_body := JSON_OBJECT('environment' VALUE :environment, 'estado' VALUE :estado,
                          'tipo' VALUE :tipo, 'page' VALUE :page, 'pageSize' VALUE :pageSize RETURNING CLOB);
    pkg_esign_document_api.pr_list_documents(
        p_client_id => pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
        p_body      => l_body,
        p_out       => l_out
    );
    htp.p(l_out);
END;]');

  -- Listado por POST con filtros en el body (los query-params no ligan de forma fiable en
  -- esta instancia ORDS -> ORDS-25001; el patron body_text es el mismo del modulo interno).
  ords.define_template(p_module_name => 'esign', p_pattern => 'documents/search');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'documents/search', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_body CLOB := :body_text;
    l_out  CLOB;
    l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
BEGIN
    pkg_esign_document_api.pr_list_documents(
        p_client_id => pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
        p_body      => l_body,
        p_out       => l_out
    );
    htp.p(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'documents/:cdc');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'documents/:cdc', p_method => 'GET',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_out  CLOB;
    l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
BEGIN
    pkg_esign_document_api.pr_get_document(
        p_client_id => pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
        p_cdc       => :cdc,
        p_out       => l_out
    );
    htp.p(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'documents/:cdc/xml');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'documents/:cdc/xml', p_method => 'GET',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_out  CLOB;
    l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
BEGIN
    owa_util.mime_header('application/xml', FALSE); owa_util.http_header_close;
    pkg_esign_document_api.pr_get_xml(
        p_client_id => pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
        p_cdc       => :cdc,
        p_out       => l_out
    );
    htp.prn(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'documents/:cdc/kude');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'documents/:cdc/kude', p_method => 'GET',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_out  CLOB;
    l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
BEGIN
    pkg_esign_kude_api.pr_get_kude(
        p_client_id => pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
        p_cdc       => :cdc,
        p_out       => l_out
    );
    htp.p(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'documents/:cdc/retry');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'documents/:cdc/retry', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE
    l_out  CLOB;
    l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
BEGIN
    pkg_esign_document_api.pr_request_retry(
        p_client_id => pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
        p_cdc       => :cdc,
        p_out       => l_out
    );
    COMMIT;
    htp.p(l_out);
END;]');

  COMMIT;
END;
/
