-- PKG_ESIGN_HTTP: paquete base y global de codigos/mensajes HTTP.
-- Centraliza los "numeros sueltos" para que handlers ORDS y paquetes usen constantes con
-- nombre en vez de literales:
--   * c_*     : codigo de estado HTTP (200, 401, ...).
--   * m_*     : frase/mensaje estandar del codigo (reason phrase de owa_util.status_line).
--   * e_*     : codigo de error de negocio para el envelope {error:{code,...}}.
--   * c_ora_* : numero de error Oracle (raise_application_error) mapeable a su HTTP.
-- No depende de ningun otro paquete (solo built-ins owa_util/htp), por eso se compila primero
-- y cualquier paquete puede referenciar sus constantes sin dependencias circulares.
CREATE OR REPLACE PACKAGE pkg_esign_http AS

  -- Codigos de estado HTTP -------------------------------------------------
  c_ok            CONSTANT PLS_INTEGER := 200;
  c_created       CONSTANT PLS_INTEGER := 201;
  c_no_content    CONSTANT PLS_INTEGER := 204;
  c_bad_request   CONSTANT PLS_INTEGER := 400;
  c_unauthorized  CONSTANT PLS_INTEGER := 401;
  c_forbidden     CONSTANT PLS_INTEGER := 403;
  c_not_found     CONSTANT PLS_INTEGER := 404;
  c_conflict      CONSTANT PLS_INTEGER := 409;
  c_unprocessable CONSTANT PLS_INTEGER := 422;
  c_server_error  CONSTANT PLS_INTEGER := 500;
  c_bad_gateway   CONSTANT PLS_INTEGER := 502;

  -- Frase estandar por codigo (reason phrase para owa_util.status_line) ----
  m_ok            CONSTANT VARCHAR2(40) := 'OK';
  m_created       CONSTANT VARCHAR2(40) := 'Created';
  m_no_content    CONSTANT VARCHAR2(40) := 'No Content';
  m_bad_request   CONSTANT VARCHAR2(40) := 'Bad Request';
  m_unauthorized  CONSTANT VARCHAR2(40) := 'Unauthorized';
  m_forbidden     CONSTANT VARCHAR2(40) := 'Forbidden';
  m_not_found     CONSTANT VARCHAR2(40) := 'Not Found';
  m_conflict      CONSTANT VARCHAR2(40) := 'Conflict';
  m_unprocessable CONSTANT VARCHAR2(40) := 'Unprocessable Entity';
  m_server_error  CONSTANT VARCHAR2(40) := 'Internal Server Error';
  m_bad_gateway   CONSTANT VARCHAR2(40) := 'Bad Gateway';

  -- Codigos de error de negocio (envelope error.code) ----------------------
  e_unauthorized  CONSTANT VARCHAR2(40) := 'UNAUTHORIZED';
  e_forbidden     CONSTANT VARCHAR2(40) := 'FORBIDDEN';
  e_key_revoked   CONSTANT VARCHAR2(40) := 'KEY_REVOKED';
  e_bad_request   CONSTANT VARCHAR2(40) := 'BAD_REQUEST';
  e_not_found     CONSTANT VARCHAR2(40) := 'NOT_FOUND';
  e_conflict      CONSTANT VARCHAR2(40) := 'CONFLICT';
  e_server_error  CONSTANT VARCHAR2(40) := 'INTERNAL_ERROR';

  -- Mensajes de error reutilizados en los handlers internos ----------------
  msg_service_token CONSTANT VARCHAR2(80) := 'service token invalido';
  msg_key_inactive  CONSTANT VARCHAR2(80) := 'API key no activa';

  -- Numeros de error Oracle (raise_application_error, rango -20000..-20999)
  -- mapeables a su codigo HTTP. Evita literales -204xx sueltos en los paquetes.
  c_ora_bad_request  CONSTANT PLS_INTEGER := -20400;
  c_ora_unauthorized CONSTANT PLS_INTEGER := -20401;
  c_ora_forbidden    CONSTANT PLS_INTEGER := -20403;
  c_ora_not_found    CONSTANT PLS_INTEGER := -20404;
  c_ora_conflict     CONSTANT PLS_INTEGER := -20409;

  -- pr_error emite una respuesta de error estandar: fija el status HTTP y escribe el
  -- envelope {"success":false,"error":{"code","message"}}. Sin dependencias externas.
  PROCEDURE pr_error(
    p_status  IN PLS_INTEGER,
    p_reason  IN VARCHAR2,
    p_code    IN VARCHAR2,
    p_message IN VARCHAR2
  );

END pkg_esign_http;
/

CREATE OR REPLACE PACKAGE BODY pkg_esign_http AS

  PROCEDURE pr_error(
    p_status  IN PLS_INTEGER,
    p_reason  IN VARCHAR2,
    p_code    IN VARCHAR2,
    p_message IN VARCHAR2
  ) IS
    l_body CLOB;
  BEGIN
    owa_util.status_line(p_status, p_reason);
    -- JSON_OBJECT es solo-SQL en PL/SQL: se arma con SELECT ... INTO.
    SELECT JSON_OBJECT(
             'success' VALUE 'false' FORMAT JSON,
             'error' VALUE JSON_OBJECT('code' VALUE p_code, 'message' VALUE p_message)
             RETURNING CLOB)
      INTO l_body
      FROM dual;
    htp.p(l_body);
  END pr_error;

END pkg_esign_http;
/
