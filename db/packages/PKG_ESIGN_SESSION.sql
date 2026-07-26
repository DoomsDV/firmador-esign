-- PKG_ESIGN_SESSION: setea/limpia el application context ESIGN_CTX.CLIENT_ID.
-- Es el UNICO paquete autorizado a escribir el contexto (CREATE CONTEXT ... USING pkg_esign_session),
-- de modo que ningun SQL suelto pueda falsear el client_id y saltarse la VPD.
CREATE OR REPLACE PACKAGE pkg_esign_session AS
  -- set_client fija el tenant activo de la sesion. Todo paquete API lo invoca luego de
  -- validar el JWT (panel) o la API key (Go), ademas de filtrar por client_id explicito.
  PROCEDURE set_client(p_client_id IN NUMBER);

  -- clear borra el contexto (deja la VPD en "deny by default": 1=2).
  PROCEDURE clear;

  -- current_client devuelve el client_id activo (o NULL).
  FUNCTION current_client RETURN NUMBER;

  -- begin_bootstrap / end_bootstrap: bypass acotado de la VPD para los lookups de
  -- autenticacion que ocurren ANTES de conocer el client_id (login por email, resolucion
  -- de API key por hash). SOLO deben usarlo los paquetes de auth confiables (definer rights),
  -- envolviendo la consulta minima y llamando end_bootstrap inmediatamente despues.
  PROCEDURE begin_bootstrap;
  PROCEDURE end_bootstrap;

  -- is_bootstrap indica si el bypass esta activo (lo lee la policy function).
  FUNCTION is_bootstrap RETURN VARCHAR2;
END pkg_esign_session;
/

CREATE OR REPLACE PACKAGE BODY pkg_esign_session AS

  PROCEDURE set_client(p_client_id IN NUMBER) IS
  BEGIN
    IF p_client_id IS NULL THEN
      raise_application_error(-20001, 'set_client: client_id no puede ser nulo');
    END IF;
    dbms_session.set_context('ESIGN_CTX', 'CLIENT_ID', TO_CHAR(p_client_id));
  END set_client;

  PROCEDURE clear IS
  BEGIN
    dbms_session.clear_context('ESIGN_CTX', NULL, 'CLIENT_ID');
  END clear;

  FUNCTION current_client RETURN NUMBER IS
  BEGIN
    RETURN TO_NUMBER(sys_context('ESIGN_CTX', 'CLIENT_ID'));
  EXCEPTION
    WHEN OTHERS THEN
      RETURN NULL;
  END current_client;

  PROCEDURE begin_bootstrap IS
  BEGIN
    dbms_session.set_context('ESIGN_CTX', 'BYPASS', 'Y');
  END begin_bootstrap;

  PROCEDURE end_bootstrap IS
  BEGIN
    dbms_session.clear_context('ESIGN_CTX', NULL, 'BYPASS');
  END end_bootstrap;

  FUNCTION is_bootstrap RETURN VARCHAR2 IS
  BEGIN
    RETURN NVL(sys_context('ESIGN_CTX', 'BYPASS'), 'N');
  END is_bootstrap;

END pkg_esign_session;
/
