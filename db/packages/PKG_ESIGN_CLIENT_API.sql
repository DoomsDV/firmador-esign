-- PKG_ESIGN_CLIENT_API: identidad del negocio + configuracion SIFEN (emisor, actividades,
-- establecimientos con su geo, puntos de expedicion, timbrado/CSC por ambiente) y el
-- correlativo de documentos. Escritura restringida a owner; el correlativo es interno (Go).
-- La VPD aisla por client_id; ademas se filtra explicito y se setea el contexto de sesion.
CREATE OR REPLACE PACKAGE pkg_esign_client_api AS

  PROCEDURE pr_get_client(p_client_id IN NUMBER, p_out OUT CLOB);
  PROCEDURE pr_upsert_emisor(p_client_id IN NUMBER, p_role IN VARCHAR2, p_body IN CLOB, p_out OUT CLOB);

  PROCEDURE pr_list_establecimiento(p_client_id IN NUMBER, p_out OUT CLOB);
  PROCEDURE pr_upsert_establecimiento(p_client_id IN NUMBER, p_role IN VARCHAR2, p_body IN CLOB, p_out OUT CLOB);
  PROCEDURE pr_deactivate_establecimiento(p_client_id IN NUMBER, p_role IN VARCHAR2, p_codigo IN VARCHAR2, p_out OUT CLOB);

  PROCEDURE pr_upsert_punto(p_client_id IN NUMBER, p_role IN VARCHAR2, p_estab_codigo IN VARCHAR2, p_body IN CLOB, p_out OUT CLOB);

  PROCEDURE pr_upsert_env(p_client_id IN NUMBER, p_role IN VARCHAR2, p_body IN CLOB, p_out OUT CLOB);

  -- Correlativo (interno, con bloqueo). Devuelve el proximo numero para el trio
  -- (establecimiento, punto, tipo DE) en el ambiente dado. Numeracion sin huecos.
  PROCEDURE pr_next_document_number(
    p_client_id       IN  NUMBER,
    p_environment     IN  VARCHAR2,
    p_establecimiento IN  VARCHAR2,
    p_punto           IN  VARCHAR2,
    p_tipo_de         IN  NUMBER,
    p_number          OUT NUMBER
  );

  -- Contexto interno para Go: emisor + establecimientos/puntos + timbrado/CSC del ambiente.
  PROCEDURE pr_get_internal_context(p_client_id IN NUMBER, p_environment IN VARCHAR2, p_out OUT CLOB);

  -- Interno (Go): CSC cifrado (hex) del ambiente. El QR requiere el CSC en claro;
  -- Go lo descifra en memoria (AES-256-GCM). El BLOB nunca sale sin cifrar.
  PROCEDURE pr_get_csc_json(p_client_id IN NUMBER, p_environment IN VARCHAR2, p_out OUT CLOB);

END pkg_esign_client_api;
/

CREATE OR REPLACE PACKAGE BODY pkg_esign_client_api AS

  PROCEDURE assert_owner(p_role IN VARCHAR2) IS
  BEGIN
    IF p_role <> 'owner' THEN
      raise_application_error(pkg_esign_http.c_ora_forbidden, 'solo el owner puede modificar esta configuracion');
    END IF;
  END assert_owner;

  PROCEDURE pr_get_client(p_client_id IN NUMBER, p_out OUT CLOB) IS
    l_data CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);
    SELECT JSON_OBJECT(
             'client_id' VALUE c.id_client,
             'business_name' VALUE c.business_name,
             'ruc' VALUE c.ruc, 'dv' VALUE c.dv, 'status' VALUE c.status,
             'emisor' VALUE (
               SELECT JSON_OBJECT('tipo_contribuyente' VALUE e.tipo_contribuyente,
                                  'tipo_regimen' VALUE e.tipo_regimen,
                                  'nombre_fantasia' VALUE e.nombre_fantasia
                                  RETURNING CLOB)
                 FROM client_emisor e WHERE e.client_id = c.id_client) FORMAT JSON,
             'actividades' VALUE (
               SELECT JSON_ARRAYAGG(JSON_OBJECT('cod' VALUE cod_act_eco, 'desc' VALUE desc_act_eco RETURNING CLOB) RETURNING CLOB)
                 FROM client_emisor_actividad WHERE client_id = c.id_client AND is_active = 1) FORMAT JSON
             RETURNING CLOB)
      INTO l_data
      FROM client c WHERE c.id_client = p_client_id;

    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_get_client;

  PROCEDURE pr_upsert_emisor(p_client_id IN NUMBER, p_role IN VARCHAR2, p_body IN CLOB, p_out OUT CLOB) IS
    l_acts JSON_ARRAY_T;
    l_obj  JSON_OBJECT_T;
  BEGIN
    assert_owner(p_role);
    pkg_esign_session.set_client(p_client_id);

    MERGE INTO client_emisor e
    USING (SELECT p_client_id AS cid FROM dual) s
    ON (e.client_id = s.cid)
    WHEN MATCHED THEN UPDATE SET
      tipo_contribuyente = NVL(json_value(p_body, '$.tipo_contribuyente'), e.tipo_contribuyente),
      tipo_regimen       = json_value(p_body, '$.tipo_regimen'),
      nombre_fantasia    = json_value(p_body, '$.nombre_fantasia')
    WHEN NOT MATCHED THEN INSERT (client_id, tipo_contribuyente, tipo_regimen, nombre_fantasia)
    VALUES (p_client_id, NVL(json_value(p_body, '$.tipo_contribuyente'), 1),
            json_value(p_body, '$.tipo_regimen'), json_value(p_body, '$.nombre_fantasia'));

    -- Reemplaza el set de actividades si viene en el body.
    IF json_exists(p_body, '$.actividades') THEN
      UPDATE client_emisor_actividad SET is_active = 0 WHERE client_id = p_client_id;
      l_acts := JSON_ARRAY_T.parse(json_query(p_body, '$.actividades'));
      FOR i IN 0 .. l_acts.get_size - 1 LOOP
        l_obj := TREAT(l_acts.get(i) AS JSON_OBJECT_T);
        -- Los metodos de JSON_OBJECT_T no pueden usarse en SQL: se pasan por variables.
        DECLARE l_cod VARCHAR2(20) := l_obj.get_string('cod'); l_desc VARCHAR2(300) := l_obj.get_string('desc');
        BEGIN
          MERGE INTO client_emisor_actividad a
          USING (SELECT p_client_id AS cid, l_cod AS cod FROM dual) s
          ON (a.client_id = s.cid AND a.cod_act_eco = s.cod)
          WHEN MATCHED THEN UPDATE SET desc_act_eco = l_desc, is_active = 1
          WHEN NOT MATCHED THEN INSERT (client_id, cod_act_eco, desc_act_eco, is_active)
          VALUES (p_client_id, l_cod, l_desc, 1);
        END;
      END LOOP;
    END IF;

    p_out := pkg_esign_util.fn_ok;
  END pr_upsert_emisor;

  PROCEDURE pr_list_establecimiento(p_client_id IN NUMBER, p_out OUT CLOB) IS
    l_data CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);
    SELECT JSON_ARRAYAGG(
             JSON_OBJECT('codigo' VALUE codigo, 'denominacion' VALUE denominacion,
                         'direccion' VALUE direccion, 'num_casa' VALUE num_casa,
                         'dep' VALUE JSON_OBJECT('cod' VALUE dep_cod, 'desc' VALUE dep_desc),
                         'dis' VALUE JSON_OBJECT('cod' VALUE dis_cod, 'desc' VALUE dis_desc),
                         'ciu' VALUE JSON_OBJECT('cod' VALUE ciu_cod, 'desc' VALUE ciu_desc),
                         'is_active' VALUE is_active,
                         'puntos' VALUE (
                           SELECT JSON_ARRAYAGG(JSON_OBJECT('codigo' VALUE p.codigo, 'descripcion' VALUE p.descripcion, 'is_active' VALUE p.is_active RETURNING CLOB) RETURNING CLOB)
                             FROM client_punto_expedicion p WHERE p.establecimiento_id = e.id_establecimiento) FORMAT JSON
                         RETURNING CLOB) RETURNING CLOB)
      INTO l_data FROM client_establecimiento e WHERE e.client_id = p_client_id;
    p_out := pkg_esign_util.fn_ok(NVL(l_data, TO_CLOB('[]')));
  END pr_list_establecimiento;

  PROCEDURE pr_upsert_establecimiento(p_client_id IN NUMBER, p_role IN VARCHAR2, p_body IN CLOB, p_out OUT CLOB) IS
    l_cod VARCHAR2(3) := json_value(p_body, '$.codigo');
  BEGIN
    assert_owner(p_role);
    pkg_esign_session.set_client(p_client_id);
    IF l_cod IS NULL THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request, 'codigo de establecimiento obligatorio');
    END IF;

    MERGE INTO client_establecimiento e
    USING (SELECT p_client_id AS cid, l_cod AS cod FROM dual) s
    ON (e.client_id = s.cid AND e.codigo = s.cod)
    WHEN MATCHED THEN UPDATE SET
      denominacion = json_value(p_body, '$.denominacion'),
      direccion    = json_value(p_body, '$.direccion'),
      num_casa     = NVL(json_value(p_body, '$.num_casa'), '0'),
      dep_cod = json_value(p_body, '$.dep.cod'), dep_desc = json_value(p_body, '$.dep.desc'),
      dis_cod = json_value(p_body, '$.dis.cod'), dis_desc = json_value(p_body, '$.dis.desc'),
      ciu_cod = json_value(p_body, '$.ciu.cod'), ciu_desc = json_value(p_body, '$.ciu.desc'),
      telefono = json_value(p_body, '$.telefono'), email = json_value(p_body, '$.email'),
      is_active = NVL(json_value(p_body, '$.is_active'), 1)
    WHEN NOT MATCHED THEN INSERT
      (client_id, codigo, denominacion, direccion, num_casa,
       dep_cod, dep_desc, dis_cod, dis_desc, ciu_cod, ciu_desc, telefono, email, is_active)
    VALUES
      (p_client_id, l_cod, json_value(p_body, '$.denominacion'), json_value(p_body, '$.direccion'),
       NVL(json_value(p_body, '$.num_casa'), '0'),
       json_value(p_body, '$.dep.cod'), json_value(p_body, '$.dep.desc'),
       json_value(p_body, '$.dis.cod'), json_value(p_body, '$.dis.desc'),
       json_value(p_body, '$.ciu.cod'), json_value(p_body, '$.ciu.desc'),
       json_value(p_body, '$.telefono'), json_value(p_body, '$.email'), 1);

    p_out := pkg_esign_util.fn_ok;
  END pr_upsert_establecimiento;

  PROCEDURE pr_deactivate_establecimiento(p_client_id IN NUMBER, p_role IN VARCHAR2, p_codigo IN VARCHAR2, p_out OUT CLOB) IS
  BEGIN
    assert_owner(p_role);
    pkg_esign_session.set_client(p_client_id);
    UPDATE client_establecimiento SET is_active = 0 WHERE client_id = p_client_id AND codigo = p_codigo;
    p_out := pkg_esign_util.fn_ok;
  END pr_deactivate_establecimiento;

  PROCEDURE pr_upsert_punto(p_client_id IN NUMBER, p_role IN VARCHAR2, p_estab_codigo IN VARCHAR2, p_body IN CLOB, p_out OUT CLOB) IS
    l_estab_id client_establecimiento.id_establecimiento%TYPE;
    l_cod VARCHAR2(3) := json_value(p_body, '$.codigo');
  BEGIN
    assert_owner(p_role);
    pkg_esign_session.set_client(p_client_id);

    BEGIN
      SELECT id_establecimiento INTO l_estab_id
        FROM client_establecimiento WHERE client_id = p_client_id AND codigo = p_estab_codigo;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN raise_application_error(pkg_esign_http.c_ora_not_found, 'establecimiento inexistente');
    END;

    MERGE INTO client_punto_expedicion p
    USING (SELECT l_estab_id AS eid, l_cod AS cod FROM dual) s
    ON (p.establecimiento_id = s.eid AND p.codigo = s.cod)
    WHEN MATCHED THEN UPDATE SET descripcion = json_value(p_body, '$.descripcion'),
                                 is_active = NVL(json_value(p_body, '$.is_active'), 1)
    WHEN NOT MATCHED THEN INSERT (establecimiento_id, client_id, codigo, descripcion, is_active)
    VALUES (l_estab_id, p_client_id, l_cod, json_value(p_body, '$.descripcion'), 1);

    p_out := pkg_esign_util.fn_ok;
  END pr_upsert_punto;

  PROCEDURE pr_upsert_env(p_client_id IN NUMBER, p_role IN VARCHAR2, p_body IN CLOB, p_out OUT CLOB) IS
    l_env VARCHAR2(4) := UPPER(json_value(p_body, '$.environment'));
    l_ct  BLOB;
    l_nonce RAW(16);
  BEGIN
    assert_owner(p_role);
    pkg_esign_session.set_client(p_client_id);
    IF l_env NOT IN ('TEST','PROD') THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request, 'environment debe ser TEST o PROD');
    END IF;

    -- csc_ciphertext/csc_nonce llegan en hex (cifrados por Go). Se guardan opacos.
    l_ct    := TO_BLOB(HEXTORAW(json_value(p_body, '$.csc_ciphertext')));
    l_nonce := HEXTORAW(json_value(p_body, '$.csc_nonce'));

    MERGE INTO client_sifen_env t
    USING (SELECT p_client_id AS cid, l_env AS env FROM dual) s
    ON (t.client_id = s.cid AND t.environment = s.env)
    WHEN MATCHED THEN UPDATE SET
      num_timbrado = json_value(p_body, '$.num_timbrado'),
      fecha_inicio_vigencia = TO_DATE(json_value(p_body, '$.fecha_inicio_vigencia'), 'YYYY-MM-DD'),
      id_csc = json_value(p_body, '$.id_csc'),
      csc_ciphertext = l_ct, csc_nonce = l_nonce,
      key_version = NVL(json_value(p_body, '$.key_version'), 1), is_active = 1
    WHEN NOT MATCHED THEN INSERT
      (client_id, environment, num_timbrado, fecha_inicio_vigencia, id_csc, csc_ciphertext, csc_nonce, key_version, is_active)
    VALUES
      (p_client_id, l_env, json_value(p_body, '$.num_timbrado'),
       TO_DATE(json_value(p_body, '$.fecha_inicio_vigencia'), 'YYYY-MM-DD'),
       json_value(p_body, '$.id_csc'), l_ct, l_nonce, NVL(json_value(p_body, '$.key_version'), 1), 1);

    p_out := pkg_esign_util.fn_ok;
  END pr_upsert_env;

  PROCEDURE pr_next_document_number(
    p_client_id       IN  NUMBER,
    p_environment     IN  VARCHAR2,
    p_establecimiento IN  VARCHAR2,
    p_punto           IN  VARCHAR2,
    p_tipo_de         IN  NUMBER,
    p_number          OUT NUMBER
  ) IS
  BEGIN
    pkg_esign_session.set_client(p_client_id);

    -- ADB con DML paralelo activo: MERGE + UPDATE sobre la misma tabla en la misma
    -- transaccion dispara ORA-12839. El hint /*+ no_parallel */ lo evita.
    -- Asegura la fila del correlativo y la bloquea; luego incrementa atomicamente.
    MERGE /*+ no_parallel */ INTO document_sequence t
    USING (SELECT p_client_id AS cid, p_environment AS env, p_establecimiento AS est,
                  p_punto AS pun, p_tipo_de AS tde FROM dual) s
    ON (t.client_id = s.cid AND t.environment = s.env AND t.establecimiento = s.est
        AND t.punto_expedicion = s.pun AND t.tipo_de = s.tde)
    WHEN NOT MATCHED THEN INSERT (client_id, environment, establecimiento, punto_expedicion, tipo_de, last_number)
    VALUES (p_client_id, p_environment, p_establecimiento, p_punto, p_tipo_de, 0);

    UPDATE /*+ no_parallel */ document_sequence
       SET last_number = last_number + 1
     WHERE client_id = p_client_id AND environment = p_environment
       AND establecimiento = p_establecimiento AND punto_expedicion = p_punto AND tipo_de = p_tipo_de
    RETURNING last_number INTO p_number;
  END pr_next_document_number;

  PROCEDURE pr_get_internal_context(p_client_id IN NUMBER, p_environment IN VARCHAR2, p_out OUT CLOB) IS
    l_data CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);
    SELECT JSON_OBJECT(
             'client_id' VALUE c.id_client,
             'business_name' VALUE c.business_name, 'ruc' VALUE c.ruc, 'dv' VALUE c.dv,
             'status' VALUE c.status,
             'environment' VALUE p_environment,
             'cert_available' VALUE CASE WHEN EXISTS (
                 SELECT 1 FROM client_certificate cc
                  WHERE cc.client_id = c.id_client AND cc.status = 'ACTIVE'
               ) THEN 'true' ELSE 'false' END FORMAT JSON,
             'emisor' VALUE (
               SELECT JSON_OBJECT('tipo_contribuyente' VALUE e.tipo_contribuyente,
                                  'tipo_regimen' VALUE e.tipo_regimen,
                                  'nombre_fantasia' VALUE e.nombre_fantasia,
                                  'actividades' VALUE (
                                    SELECT JSON_ARRAYAGG(JSON_OBJECT('cod' VALUE cod_act_eco, 'desc' VALUE desc_act_eco RETURNING CLOB) RETURNING CLOB)
                                      FROM client_emisor_actividad WHERE client_id = c.id_client AND is_active = 1) FORMAT JSON
                                  RETURNING CLOB)
                 FROM client_emisor e WHERE e.client_id = c.id_client) FORMAT JSON,
             'establecimientos' VALUE (
               SELECT JSON_ARRAYAGG(
                        JSON_OBJECT('codigo' VALUE est.codigo, 'denominacion' VALUE est.denominacion,
                                    'direccion' VALUE est.direccion, 'num_casa' VALUE est.num_casa,
                                    'dep' VALUE JSON_OBJECT('cod' VALUE est.dep_cod, 'desc' VALUE est.dep_desc),
                                    'dis' VALUE JSON_OBJECT('cod' VALUE est.dis_cod, 'desc' VALUE est.dis_desc),
                                    'ciu' VALUE JSON_OBJECT('cod' VALUE est.ciu_cod, 'desc' VALUE est.ciu_desc),
                                    'telefono' VALUE est.telefono, 'email' VALUE est.email,
                                    'puntos' VALUE (
                                      SELECT JSON_ARRAYAGG(pt.codigo ORDER BY pt.codigo)
                                        FROM client_punto_expedicion pt
                                       WHERE pt.establecimiento_id = est.id_establecimiento AND pt.is_active = 1) FORMAT JSON
                                    RETURNING CLOB) RETURNING CLOB)
                 FROM client_establecimiento est
                WHERE est.client_id = c.id_client AND est.is_active = 1) FORMAT JSON,
             'sifen_env' VALUE (
               SELECT JSON_OBJECT('num_timbrado' VALUE se.num_timbrado,
                                  'fecha_inicio_vigencia' VALUE TO_CHAR(se.fecha_inicio_vigencia, 'YYYY-MM-DD'),
                                  'id_csc' VALUE se.id_csc, 'key_version' VALUE se.key_version
                                  RETURNING CLOB)
                 FROM client_sifen_env se
                WHERE se.client_id = c.id_client AND se.environment = p_environment AND se.is_active = 1) FORMAT JSON
             RETURNING CLOB)
      INTO l_data
      FROM client c WHERE c.id_client = p_client_id;

    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_get_internal_context;

  PROCEDURE pr_get_csc_json(p_client_id IN NUMBER, p_environment IN VARCHAR2, p_out OUT CLOB) IS
    l_ct     BLOB;
    l_nonce  RAW(16);
    l_id_csc VARCHAR2(4);
    l_kv     NUMBER;
    l_cthex  CLOB;
    l_data   CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);
    BEGIN
      SELECT csc_ciphertext, csc_nonce, id_csc, key_version
        INTO l_ct, l_nonce, l_id_csc, l_kv
        FROM client_sifen_env
       WHERE client_id = p_client_id AND environment = UPPER(p_environment) AND is_active = 1;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN
        raise_application_error(pkg_esign_http.c_ora_not_found, 'el cliente no tiene CSC configurado para el ambiente '||p_environment);
    END;
    -- fn_blob_hex tiene efectos (DBMS_LOB) => precomputar en variable antes del SQL.
    l_cthex := pkg_esign_util.fn_blob_hex(l_ct);
    SELECT JSON_OBJECT(
        'id_csc'         VALUE l_id_csc,
        'csc_ciphertext' VALUE l_cthex,
        'csc_nonce'      VALUE LOWER(RAWTOHEX(l_nonce)),
        'key_version'    VALUE l_kv RETURNING CLOB)
      INTO l_data FROM dual;
    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_get_csc_json;

END pkg_esign_client_api;
/
