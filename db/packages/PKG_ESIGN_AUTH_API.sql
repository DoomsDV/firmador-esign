-- PKG_ESIGN_AUTH_API: flujo de autenticacion del panel (patron Stripe: persona <-> negocios).
-- Contrato JSON in/out para que los handlers ORDS sean delgados. Un email puede pertenecer a
-- varios clients; login devuelve la lista para elegir y select-client emite el JWT con ese
-- client_id + role. El registro crea client + users + membresia owner + client_emisor 1:1.
CREATE OR REPLACE PACKAGE pkg_esign_auth_api AS

  PROCEDURE pr_register(p_body IN CLOB, p_out OUT CLOB);
  PROCEDURE pr_login(p_body IN CLOB, p_out OUT CLOB);
  PROCEDURE pr_select_client(p_user_id IN NUMBER, p_client_id IN NUMBER, p_out OUT CLOB);
  PROCEDURE pr_list_my_clients(p_user_id IN NUMBER, p_out OUT CLOB);
  PROCEDURE pr_refresh(p_body IN CLOB, p_out OUT CLOB);
  PROCEDURE pr_logout(p_body IN CLOB, p_out OUT CLOB);
  PROCEDURE pr_me(p_authorization IN VARCHAR2, p_out OUT CLOB);

  PROCEDURE pr_invite_user(p_authorization IN VARCHAR2, p_body IN CLOB, p_out OUT CLOB);
  PROCEDURE pr_accept_invitation(p_body IN CLOB, p_out OUT CLOB);

END pkg_esign_auth_api;
/

CREATE OR REPLACE PACKAGE BODY pkg_esign_auth_api AS

  -- fn_clients_json arma el arreglo de negocios de un usuario (id, business_name, role).
  FUNCTION fn_clients_json(p_user_id IN NUMBER) RETURN CLOB IS
    l_json CLOB;
  BEGIN
    pkg_esign_session.begin_bootstrap;
    SELECT JSON_ARRAYAGG(
             JSON_OBJECT('client_id' VALUE c.id_client,
                         'business_name' VALUE c.business_name,
                         'ruc' VALUE c.ruc,
                         'role' VALUE cu.role
                         RETURNING CLOB)
             RETURNING CLOB)
      INTO l_json
      FROM client_user cu
      JOIN client c ON c.id_client = cu.client_id
     WHERE cu.user_id = p_user_id AND cu.is_active = 1;
    pkg_esign_session.end_bootstrap;
    RETURN NVL(l_json, TO_CLOB('[]'));
  END fn_clients_json;

  PROCEDURE pr_register(p_body IN CLOB, p_out OUT CLOB) IS
    l_email    VARCHAR2(150) := LOWER(json_value(p_body, '$.email'));
    l_password VARCHAR2(200) := json_value(p_body, '$.password');
    l_first    VARCHAR2(50)  := json_value(p_body, '$.first_name');
    l_last     VARCHAR2(50)  := json_value(p_body, '$.last_name');
    l_bname    VARCHAR2(255) := json_value(p_body, '$.business_name');
    l_ruc      VARCHAR2(15)  := json_value(p_body, '$.ruc');
    l_dv       NUMBER        := json_value(p_body, '$.dv');
    l_tipcont  NUMBER        := NVL(json_value(p_body, '$.tipo_contribuyente'), 1);
    l_salt     VARCHAR2(64);
    l_user_id  users.id_user%TYPE;
    l_client_id client.id_client%TYPE;
  BEGIN
    IF l_email IS NULL OR l_password IS NULL OR l_bname IS NULL OR l_ruc IS NULL THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request, 'email, password, business_name y ruc son obligatorios');
    END IF;

    l_salt := pkg_esign_util.fn_new_salt;

    INSERT INTO users (email, password_hash, password_salt, first_name, last_name, is_active)
    VALUES (l_email, pkg_esign_util.fn_hash_password(l_password, l_salt), l_salt, l_first, l_last, 1)
    RETURNING id_user INTO l_user_id;

    -- La creacion del client necesita bypass: aun no hay tenant en el contexto.
    pkg_esign_session.begin_bootstrap;
    INSERT INTO client (business_name, ruc, dv, status)
    VALUES (l_bname, l_ruc, l_dv, 'ACTIVE')
    RETURNING id_client INTO l_client_id;

    INSERT INTO client_user (client_id, user_id, role, is_active)
    VALUES (l_client_id, l_user_id, 'owner', 1);

    INSERT INTO client_emisor (client_id, tipo_contribuyente, tipo_regimen, nombre_fantasia)
    VALUES (l_client_id, l_tipcont, json_value(p_body, '$.tipo_regimen'), json_value(p_body, '$.nombre_fantasia'));
    pkg_esign_session.end_bootstrap;

    DECLARE l_data CLOB; BEGIN
      SELECT JSON_OBJECT('user_id' VALUE l_user_id, 'client_id' VALUE l_client_id, 'role' VALUE 'owner' RETURNING CLOB)
        INTO l_data FROM dual;
      p_out := pkg_esign_util.fn_ok(l_data);
    END;
  EXCEPTION
    WHEN DUP_VAL_ON_INDEX THEN
      pkg_esign_session.end_bootstrap;
      raise_application_error(pkg_esign_http.c_ora_conflict, 'el email ya esta registrado');
  END pr_register;

  PROCEDURE pr_login(p_body IN CLOB, p_out OUT CLOB) IS
    l_email    VARCHAR2(150) := LOWER(json_value(p_body, '$.email'));
    l_password VARCHAR2(200) := json_value(p_body, '$.password');
    l_user_id  users.id_user%TYPE;
    l_hash     users.password_hash%TYPE;
    l_salt     users.password_salt%TYPE;
    l_active   users.is_active%TYPE;
  BEGIN
    BEGIN
      SELECT id_user, password_hash, password_salt, is_active
        INTO l_user_id, l_hash, l_salt, l_active
        FROM users WHERE email = l_email;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN
        raise_application_error(pkg_esign_http.c_ora_unauthorized, 'credenciales invalidas');
    END;

    IF l_active = 0 OR NOT pkg_esign_util.fn_verify_password(l_password, l_salt, l_hash) THEN
      raise_application_error(pkg_esign_http.c_ora_unauthorized, 'credenciales invalidas');
    END IF;

    -- Devuelve la lista de negocios para que el front elija (select-client emite el JWT).
    DECLARE l_clients CLOB; l_data CLOB; BEGIN
      l_clients := fn_clients_json(l_user_id);
      SELECT JSON_OBJECT('user_id' VALUE l_user_id, 'clients' VALUE l_clients FORMAT JSON RETURNING CLOB)
        INTO l_data FROM dual;
      p_out := pkg_esign_util.fn_ok(l_data);
    END;
  END pr_login;

  PROCEDURE pr_select_client(p_user_id IN NUMBER, p_client_id IN NUMBER, p_out OUT CLOB) IS
    l_role    client_user.role%TYPE;
    l_access  VARCHAR2(4000);
    l_refresh VARCHAR2(200);
    l_exp     NUMBER;
  BEGIN
    pkg_esign_session.begin_bootstrap;
    BEGIN
      SELECT role INTO l_role
        FROM client_user
       WHERE user_id = p_user_id AND client_id = p_client_id AND is_active = 1;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN
        pkg_esign_session.end_bootstrap;
        raise_application_error(pkg_esign_http.c_ora_forbidden, 'el usuario no pertenece a ese negocio');
    END;
    pkg_esign_session.end_bootstrap;

    pkg_esign_jwt.pr_generate_auth_tokens(p_user_id, p_client_id, l_role, l_access, l_refresh, l_exp);

    DECLARE l_data CLOB; BEGIN
      SELECT JSON_OBJECT('access_token' VALUE l_access, 'refresh_token' VALUE l_refresh,
                         'expires_in' VALUE l_exp, 'client_id' VALUE p_client_id, 'role' VALUE l_role RETURNING CLOB)
        INTO l_data FROM dual;
      p_out := pkg_esign_util.fn_ok(l_data);
    END;
  END pr_select_client;

  PROCEDURE pr_list_my_clients(p_user_id IN NUMBER, p_out OUT CLOB) IS
    l_clients CLOB;
  BEGIN
    l_clients := fn_clients_json(p_user_id);
    p_out := pkg_esign_util.fn_ok(l_clients);
  END pr_list_my_clients;

  PROCEDURE pr_refresh(p_body IN CLOB, p_out OUT CLOB) IS
    l_access  VARCHAR2(4000);
    l_refresh VARCHAR2(200);
    l_exp     NUMBER;
    l_data    CLOB;
  BEGIN
    pkg_esign_jwt.pr_refresh(json_value(p_body, '$.refresh_token'), l_access, l_refresh, l_exp);
    SELECT JSON_OBJECT('access_token' VALUE l_access, 'refresh_token' VALUE l_refresh, 'expires_in' VALUE l_exp RETURNING CLOB)
      INTO l_data FROM dual;
    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_refresh;

  PROCEDURE pr_logout(p_body IN CLOB, p_out OUT CLOB) IS
  BEGIN
    pkg_esign_jwt.pr_revoke(json_value(p_body, '$.refresh_token'));
    p_out := pkg_esign_util.fn_ok;
  END pr_logout;

  PROCEDURE pr_me(p_authorization IN VARCHAR2, p_out OUT CLOB) IS
    l_user_id   NUMBER := pkg_esign_util.fn_get_user_id_from_jwt(p_authorization);
    l_client_id NUMBER := pkg_esign_util.fn_get_client_id_from_jwt(p_authorization);
    l_role      VARCHAR2(20) := pkg_esign_util.fn_get_role_from_jwt(p_authorization);
    l_email     users.email%TYPE;
    l_first     users.first_name%TYPE;
    l_last      users.last_name%TYPE;
  BEGIN
    SELECT email, first_name, last_name INTO l_email, l_first, l_last
      FROM users WHERE id_user = l_user_id;
    DECLARE l_data CLOB; BEGIN
      SELECT JSON_OBJECT('user_id' VALUE l_user_id, 'email' VALUE l_email,
                         'first_name' VALUE l_first, 'last_name' VALUE l_last,
                         'client_id' VALUE l_client_id, 'role' VALUE l_role RETURNING CLOB)
        INTO l_data FROM dual;
      p_out := pkg_esign_util.fn_ok(l_data);
    END;
  END pr_me;

  PROCEDURE pr_invite_user(p_authorization IN VARCHAR2, p_body IN CLOB, p_out OUT CLOB) IS
    l_role      VARCHAR2(20) := pkg_esign_util.fn_get_role_from_jwt(p_authorization);
    l_client_id NUMBER       := pkg_esign_util.fn_get_client_id_from_jwt(p_authorization);
    l_inv_email VARCHAR2(150) := LOWER(json_value(p_body, '$.email'));
    l_inv_role  VARCHAR2(20)  := json_value(p_body, '$.role');
    l_user_id   users.id_user%TYPE;
  BEGIN
    IF l_role <> 'owner' THEN
      raise_application_error(pkg_esign_http.c_ora_forbidden, 'solo el owner puede invitar usuarios');
    END IF;
    IF l_inv_role NOT IN ('owner','developer','analyst') THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request, 'role invalido');
    END IF;

    pkg_esign_session.set_client(l_client_id);

    -- Si el invitado ya existe como persona, se agrega la membresia directamente.
    -- Si no, se deja registrada la invitacion como membresia inactiva (acepta luego con su alta).
    BEGIN
      pkg_esign_session.begin_bootstrap;
      SELECT id_user INTO l_user_id FROM users WHERE email = l_inv_email;
      pkg_esign_session.end_bootstrap;

      INSERT INTO client_user (client_id, user_id, role, is_active)
      VALUES (l_client_id, l_user_id, l_inv_role, 1);
      DECLARE l_data CLOB; BEGIN
        SELECT JSON_OBJECT('status' VALUE 'MEMBERSHIP_ADDED' RETURNING CLOB) INTO l_data FROM dual;
        p_out := pkg_esign_util.fn_ok(l_data);
      END;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN
        pkg_esign_session.end_bootstrap;
        DECLARE l_data CLOB; BEGIN
          SELECT JSON_OBJECT('status' VALUE 'INVITE_PENDING', 'email' VALUE l_inv_email RETURNING CLOB) INTO l_data FROM dual;
          p_out := pkg_esign_util.fn_ok(l_data);
        END;
      WHEN DUP_VAL_ON_INDEX THEN
        raise_application_error(pkg_esign_http.c_ora_conflict, 'el usuario ya es miembro de este negocio');
    END;
  END pr_invite_user;

  PROCEDURE pr_accept_invitation(p_body IN CLOB, p_out OUT CLOB) IS
  BEGIN
    -- Placeholder para el flujo de invitaciones por token (tabla client_invitation opcional).
    p_out := pkg_esign_util.fn_err('NOT_IMPLEMENTED', 'flujo de invitacion por token pendiente');
  END pr_accept_invitation;

END pkg_esign_auth_api;
/
