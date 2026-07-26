-- fn_tenant_policy: predicado de fila para las tablas que llevan columna client_id.
-- Deny by default: si no hay tenant en el contexto, 1=2 (no ve nada). Es defensa en
-- profundidad: cada paquete ademas filtra por client_id explicito.
CREATE OR REPLACE FUNCTION fn_tenant_policy(
  pi_schema IN VARCHAR2,
  pi_object IN VARCHAR2
) RETURN VARCHAR2 IS
BEGIN
  -- Bypass acotado para lookups de autenticacion (login/resolucion de key) previos al tenant.
  IF sys_context('ESIGN_CTX', 'BYPASS') = 'Y' THEN
    RETURN '1=1';
  END IF;
  IF sys_context('ESIGN_CTX', 'CLIENT_ID') IS NULL THEN
    RETURN '1=2';
  END IF;
  RETURN 'client_id = sys_context(''ESIGN_CTX'',''CLIENT_ID'')';
END fn_tenant_policy;
/

-- fn_client_policy: igual pero para la tabla client, cuya PK es id_client (no client_id).
CREATE OR REPLACE FUNCTION fn_client_policy(
  pi_schema IN VARCHAR2,
  pi_object IN VARCHAR2
) RETURN VARCHAR2 IS
BEGIN
  IF sys_context('ESIGN_CTX', 'BYPASS') = 'Y' THEN
    RETURN '1=1';
  END IF;
  IF sys_context('ESIGN_CTX', 'CLIENT_ID') IS NULL THEN
    RETURN '1=2';
  END IF;
  RETURN 'id_client = sys_context(''ESIGN_CTX'',''CLIENT_ID'')';
END fn_client_policy;
/
