-- PKG_ESIGN_JWT: emision/renovacion/revocacion de tokens del panel.
-- El access token lleva los claims user_id / client_id / role del negocio elegido.
-- El refresh token se persiste en user_session (atado a la membresia client_user).
CREATE OR REPLACE PACKAGE pkg_esign_jwt AS

  -- pr_generate_auth_tokens emite access+refresh para una membresia concreta
  -- (client_user = user en un client con su rol). p_client_id/p_role deben corresponder
  -- a una fila activa de client_user del usuario.
  PROCEDURE pr_generate_auth_tokens(
    p_user_id        IN  NUMBER,
    p_client_id      IN  NUMBER,
    p_role           IN  VARCHAR2,
    p_access_token   OUT VARCHAR2,
    p_refresh_token  OUT VARCHAR2,
    p_expires_in     OUT NUMBER
  );

  -- pr_refresh valida el refresh token vigente y emite un nuevo par.
  PROCEDURE pr_refresh(
    p_refresh_token  IN  VARCHAR2,
    p_access_token   OUT VARCHAR2,
    p_new_refresh    OUT VARCHAR2,
    p_expires_in     OUT NUMBER
  );

  PROCEDURE pr_revoke(p_refresh_token IN VARCHAR2);

END pkg_esign_jwt;
/

CREATE OR REPLACE PACKAGE BODY pkg_esign_jwt AS

  FUNCTION fn_access_exp_sec RETURN NUMBER IS
  BEGIN
    RETURN TO_NUMBER(NVL(pkg_esign_util.fn_get_parameter('JWT_ACCESS_EXP_SEC'), '3600'));
  END fn_access_exp_sec;

  FUNCTION fn_refresh_exp_days RETURN NUMBER IS
  BEGIN
    RETURN TO_NUMBER(NVL(pkg_esign_util.fn_get_parameter('JWT_REFRESH_EXP_DAYS'), '30'));
  END fn_refresh_exp_days;

  PROCEDURE pr_generate_auth_tokens(
    p_user_id        IN  NUMBER,
    p_client_id      IN  NUMBER,
    p_role           IN  VARCHAR2,
    p_access_token   OUT VARCHAR2,
    p_refresh_token  OUT VARCHAR2,
    p_expires_in     OUT NUMBER
  ) IS
    l_cu_id       client_user.id_client_user%TYPE;
    l_claims      VARCHAR2(1000);
    l_refresh_days NUMBER := fn_refresh_exp_days;
  BEGIN
    -- Resolver la membresia (con bypass VPD porque aun no hay tenant seteado en refresh/login).
    pkg_esign_session.begin_bootstrap;
    SELECT id_client_user INTO l_cu_id
      FROM client_user
     WHERE user_id = p_user_id AND client_id = p_client_id AND is_active = 1;
    pkg_esign_session.end_bootstrap;

    l_claims := '"user_id":' || p_user_id
             || ',"client_id":' || p_client_id
             || ',"role":"' || p_role || '"';

    p_access_token := apex_jwt.encode(
      p_iss           => pkg_esign_util.fn_get_parameter('JWT_ISSUER'),
      p_aud           => pkg_esign_util.fn_get_parameter('JWT_AUDIENCE'),
      p_sub           => TO_CHAR(p_user_id),
      p_exp_sec       => fn_access_exp_sec,
      p_other_claims  => l_claims,
      p_signature_key => utl_raw.cast_to_raw(pkg_esign_util.fn_get_parameter('JWT_TOKEN'))
    );

    p_refresh_token := pkg_esign_util.fn_random_hex(32);
    p_expires_in    := fn_access_exp_sec;

    pkg_esign_session.begin_bootstrap;
    INSERT INTO user_session (client_user_id, refresh_token, expires_at, is_revoked)
    VALUES (l_cu_id, p_refresh_token,
            SYSTIMESTAMP + NUMTODSINTERVAL(l_refresh_days, 'DAY'), 0);
    pkg_esign_session.end_bootstrap;
  END pr_generate_auth_tokens;

  PROCEDURE pr_refresh(
    p_refresh_token  IN  VARCHAR2,
    p_access_token   OUT VARCHAR2,
    p_new_refresh    OUT VARCHAR2,
    p_expires_in     OUT NUMBER
  ) IS
    l_user_id   users.id_user%TYPE;
    l_client_id client.id_client%TYPE;
    l_role      client_user.role%TYPE;
  BEGIN
    pkg_esign_session.begin_bootstrap;
    SELECT cu.user_id, cu.client_id, cu.role
      INTO l_user_id, l_client_id, l_role
      FROM user_session us
      JOIN client_user cu ON cu.id_client_user = us.client_user_id
     WHERE us.refresh_token = p_refresh_token
       AND us.is_revoked = 0
       AND us.expires_at > SYSTIMESTAMP
       AND cu.is_active = 1;

    -- Rotacion: revocar el refresh usado.
    UPDATE user_session SET is_revoked = 1 WHERE refresh_token = p_refresh_token;
    pkg_esign_session.end_bootstrap;

    pr_generate_auth_tokens(l_user_id, l_client_id, l_role,
                            p_access_token, p_new_refresh, p_expires_in);
  EXCEPTION
    WHEN NO_DATA_FOUND THEN
      pkg_esign_session.end_bootstrap;
      raise_application_error(-20401, 'refresh token invalido o expirado');
  END pr_refresh;

  PROCEDURE pr_revoke(p_refresh_token IN VARCHAR2) IS
  BEGIN
    pkg_esign_session.begin_bootstrap;
    UPDATE user_session SET is_revoked = 1 WHERE refresh_token = p_refresh_token;
    pkg_esign_session.end_bootstrap;
  END pr_revoke;

END pkg_esign_jwt;
/
