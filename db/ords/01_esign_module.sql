-- Modulo ORDS 'esign' (/api/v1/) para el panel. Handlers delgados: leen Authorization via
-- owa_util.get_cgi_env('AUTHORIZATION'), el body via l_body := :body_text (CLOB) y delegan en
-- los paquetes PKG_ESIGN_*. La salida JSON se emite con htp.p. La autorizacion real (JWT) la
-- valida cada paquete.
-- Nota: el cuerpo se lee UNA sola vez en l_body := :body_text (CLOB); :body es BLOB y no liga
-- con json_value/CLOB, y referenciar :body_text mas de una vez en el mismo handler falla
-- (ORDS-25001). Mismo patron que el modulo interno 02_*.
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
DECLARE l_body CLOB := :body_text; l_out CLOB; BEGIN
  pkg_esign_auth_api.pr_register(l_body, l_out);
  htp.p(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'auth/login');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'auth/login', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE l_body CLOB := :body_text; l_out CLOB; BEGIN
  pkg_esign_auth_api.pr_login(l_body, l_out);
  htp.p(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'auth/select-client');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'auth/select-client', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE l_body CLOB := :body_text; l_out CLOB; BEGIN
  pkg_esign_auth_api.pr_select_client(
    TO_NUMBER(json_value(l_body, '$.user_id')),
    TO_NUMBER(json_value(l_body, '$.client_id')), l_out);
  htp.p(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'auth/my-clients');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'auth/my-clients', p_method => 'GET',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE l_out CLOB; l_uid NUMBER; BEGIN
  l_uid := pkg_esign_util.fn_get_user_id_from_jwt(owa_util.get_cgi_env('AUTHORIZATION'));
  pkg_esign_auth_api.pr_list_my_clients(l_uid, l_out);
  htp.p(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'auth/refresh');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'auth/refresh', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE l_body CLOB := :body_text; l_out CLOB; BEGIN pkg_esign_auth_api.pr_refresh(l_body, l_out); htp.p(l_out); END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'auth/logout');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'auth/logout', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE l_body CLOB := :body_text; l_out CLOB; BEGIN pkg_esign_auth_api.pr_logout(l_body, l_out); htp.p(l_out); END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'auth/me');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'auth/me', p_method => 'GET',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE l_out CLOB; BEGIN
  pkg_esign_auth_api.pr_me(owa_util.get_cgi_env('AUTHORIZATION'), l_out); htp.p(l_out);
END;]');

  ---------------------------------------------------------------------------
  -- CLIENT / EMISOR
  ---------------------------------------------------------------------------
  ords.define_template(p_module_name => 'esign', p_pattern => 'client');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'client', p_method => 'GET',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE l_out CLOB; l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION'); BEGIN
  pkg_esign_client_api.pr_get_client(pkg_esign_util.fn_get_client_id_from_jwt(l_auth), l_out);
  htp.p(l_out);
END;]');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'client', p_method => 'PUT',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE l_body CLOB := :body_text; l_out CLOB; l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION'); BEGIN
  pkg_esign_client_api.pr_upsert_emisor(
    pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
    pkg_esign_util.fn_get_role_from_jwt(l_auth), l_body, l_out);
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
DECLARE l_out CLOB; l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION'); BEGIN
  pkg_esign_client_api.pr_list_establecimiento(pkg_esign_util.fn_get_client_id_from_jwt(l_auth), l_out);
  htp.p(l_out);
END;]');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'establecimientos', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE l_body CLOB := :body_text; l_out CLOB; l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION'); BEGIN
  pkg_esign_client_api.pr_upsert_establecimiento(
    pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
    pkg_esign_util.fn_get_role_from_jwt(l_auth), l_body, l_out);
  htp.p(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'establecimientos/:codigo/puntos');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'establecimientos/:codigo/puntos', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE l_body CLOB := :body_text; l_out CLOB; l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION'); BEGIN
  pkg_esign_client_api.pr_upsert_punto(
    pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
    pkg_esign_util.fn_get_role_from_jwt(l_auth), :codigo, l_body, l_out);
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
DECLARE l_body CLOB := :body_text; l_out CLOB; l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION'); BEGIN
  pkg_esign_client_api.pr_upsert_env(
    pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
    pkg_esign_util.fn_get_role_from_jwt(l_auth), l_body, l_out);
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
DECLARE l_out CLOB; l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION'); BEGIN
  pkg_esign_apikey_api.pr_get_keys_meta(pkg_esign_util.fn_get_client_id_from_jwt(l_auth), l_out);
  htp.p(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'api-keys/:env/rotate');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'api-keys/:env/rotate', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE l_out CLOB; l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION'); BEGIN
  pkg_esign_apikey_api.pr_rotate_key(
    pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
    pkg_esign_util.fn_get_role_from_jwt(l_auth), UPPER(:env), l_out);
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
DECLARE l_out CLOB; l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION'); BEGIN
  pkg_esign_cert_api.pr_get_meta(pkg_esign_util.fn_get_client_id_from_jwt(l_auth), l_out);
  htp.p(l_out);
END;]');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'certificate', p_method => 'POST',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE l_body CLOB := :body_text; l_out CLOB; l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION'); BEGIN
  pkg_esign_cert_api.pr_put_certificate(
    pkg_esign_util.fn_get_client_id_from_jwt(l_auth),
    pkg_esign_util.fn_get_role_from_jwt(l_auth), l_body, l_out);
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
  l_out CLOB; l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION');
  l_body CLOB;
BEGIN
  l_body := JSON_OBJECT('environment' VALUE :environment, 'estado' VALUE :estado,
                        'tipo' VALUE :tipo, 'page' VALUE :page, 'pageSize' VALUE :pageSize RETURNING CLOB);
  pkg_esign_document_api.pr_list_documents(pkg_esign_util.fn_get_client_id_from_jwt(l_auth), l_body, l_out);
  htp.p(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'documents/:cdc');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'documents/:cdc', p_method => 'GET',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE l_out CLOB; l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION'); BEGIN
  pkg_esign_document_api.pr_get_document(pkg_esign_util.fn_get_client_id_from_jwt(l_auth), :cdc, l_out);
  htp.p(l_out);
END;]');

  ords.define_template(p_module_name => 'esign', p_pattern => 'documents/:cdc/xml');
  ords.define_handler(
    p_module_name => 'esign', p_pattern => 'documents/:cdc/xml', p_method => 'GET',
    p_source_type => ords.source_type_plsql,
    p_source => q'[
DECLARE l_out CLOB; l_auth VARCHAR2(4000) := owa_util.get_cgi_env('AUTHORIZATION'); BEGIN
  owa_util.mime_header('application/xml', FALSE); owa_util.http_header_close;
  pkg_esign_document_api.pr_get_xml(pkg_esign_util.fn_get_client_id_from_jwt(l_auth), :cdc, l_out);
  htp.prn(l_out);
END;]');

  COMMIT;
END;
/
