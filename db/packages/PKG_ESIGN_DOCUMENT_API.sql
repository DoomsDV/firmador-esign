-- PKG_ESIGN_DOCUMENT_API: persistencia y consulta de documentos emitidos.
-- pr_register_document hace upsert por CDC (Go lo llama tras firmar/enviar). Las consultas
-- (list/get/xml) son para el panel; todos los roles pueden leer, la VPD aisla por client.
CREATE OR REPLACE PACKAGE pkg_esign_document_api AS

  PROCEDURE pr_register_document(p_client_id IN NUMBER, p_body IN CLOB, p_out OUT CLOB);
  PROCEDURE pr_register_event(p_client_id IN NUMBER, p_body IN CLOB, p_out OUT CLOB);

  PROCEDURE pr_list_documents(p_client_id IN NUMBER, p_body IN CLOB, p_out OUT CLOB);
  PROCEDURE pr_get_document(p_client_id IN NUMBER, p_cdc IN VARCHAR2, p_out OUT CLOB);
  PROCEDURE pr_get_xml(p_client_id IN NUMBER, p_cdc IN VARCHAR2, p_out OUT CLOB);

END pkg_esign_document_api;
/

CREATE OR REPLACE PACKAGE BODY pkg_esign_document_api AS

  PROCEDURE pr_register_document(p_client_id IN NUMBER, p_body IN CLOB, p_out OUT CLOB) IS
    l_cdc  VARCHAR2(44) := json_value(p_body, '$.cdc');
    l_doc_id document.id_document%TYPE;
    l_xml  CLOB := json_value(p_body, '$.xml_firmado' RETURNING CLOB);
    l_qr   VARCHAR2(1000) := json_value(p_body, '$.qr_url');
    l_data CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);

    MERGE /*+ no_parallel */ INTO document d
    USING (SELECT p_client_id AS cid, l_cdc AS cdc FROM dual) s
    ON (d.client_id = s.cid AND d.cdc = s.cdc)
    WHEN MATCHED THEN UPDATE SET
      estado = json_value(p_body, '$.estado'),
      cod_res = json_value(p_body, '$.cod_res'),
      prot_aut = json_value(p_body, '$.prot_aut'),
      mensaje_res = json_value(p_body, '$.mensaje_res')
    WHEN NOT MATCHED THEN INSERT
      (client_id, environment, tipo_de, cdc, num_documento, establecimiento, punto_expedicion,
       receptor_nombre, receptor_doc, moneda, total_operacion, estado, cod_res, prot_aut, mensaje_res, fecha_emision)
    VALUES
      (p_client_id, json_value(p_body, '$.environment'), json_value(p_body, '$.tipo_de'),
       l_cdc, json_value(p_body, '$.num_documento'),
       json_value(p_body, '$.establecimiento'), json_value(p_body, '$.punto_expedicion'),
       json_value(p_body, '$.receptor_nombre'), json_value(p_body, '$.receptor_doc'),
       json_value(p_body, '$.moneda'), json_value(p_body, '$.total_operacion'),
       json_value(p_body, '$.estado'), json_value(p_body, '$.cod_res'),
       json_value(p_body, '$.prot_aut'), json_value(p_body, '$.mensaje_res'), SYSTIMESTAMP);

    SELECT id_document INTO l_doc_id FROM document WHERE client_id = p_client_id AND cdc = l_cdc;

    -- Persistir XML/QR si vienen (regla 0141: es el XML tal cual se envio a SIFEN).
    IF l_xml IS NOT NULL OR l_qr IS NOT NULL THEN
      MERGE /*+ no_parallel */ INTO document_xml x
      USING (SELECT l_doc_id AS did FROM dual) s
      ON (x.document_id = s.did)
      WHEN MATCHED THEN UPDATE SET xml_firmado = l_xml, qr_url = l_qr
      WHEN NOT MATCHED THEN INSERT (document_id, client_id, xml_firmado, qr_url)
      VALUES (l_doc_id, p_client_id, l_xml, l_qr);
    END IF;

    SELECT JSON_OBJECT('document_id' VALUE l_doc_id, 'cdc' VALUE l_cdc RETURNING CLOB) INTO l_data FROM dual;
    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_register_document;

  PROCEDURE pr_register_event(p_client_id IN NUMBER, p_body IN CLOB, p_out OUT CLOB) IS
    l_doc_id document.id_document%TYPE;
    l_cdc VARCHAR2(44) := json_value(p_body, '$.cdc');
  BEGIN
    pkg_esign_session.set_client(p_client_id);

    IF l_cdc IS NOT NULL THEN
      BEGIN
        SELECT id_document INTO l_doc_id FROM document WHERE client_id = p_client_id AND cdc = l_cdc;
      EXCEPTION WHEN NO_DATA_FOUND THEN l_doc_id := NULL; END;
    END IF;

    INSERT /*+ no_parallel */ INTO document_event (document_id, client_id, tipo_evento, estado, cod_res, prot_aut, motivo)
    VALUES (l_doc_id, p_client_id, json_value(p_body, '$.tipo_evento'),
            json_value(p_body, '$.estado'), json_value(p_body, '$.cod_res'),
            json_value(p_body, '$.prot_aut'), json_value(p_body, '$.motivo'));

    -- La cancelacion aceptada marca el documento como CANCELADO.
    IF json_value(p_body, '$.tipo_evento') = 'CANCELACION'
       AND json_value(p_body, '$.cod_res') = '0600' AND l_doc_id IS NOT NULL THEN
      UPDATE /*+ no_parallel */ document SET estado = 'CANCELADO' WHERE id_document = l_doc_id;
    END IF;

    p_out := pkg_esign_util.fn_ok;
  END pr_register_event;

  PROCEDURE pr_list_documents(p_client_id IN NUMBER, p_body IN CLOB, p_out OUT CLOB) IS
    l_env    VARCHAR2(4)  := json_value(p_body, '$.environment');
    l_estado VARCHAR2(20) := json_value(p_body, '$.estado');
    l_tipo   NUMBER       := json_value(p_body, '$.tipo');
    l_page   NUMBER       := NVL(json_value(p_body, '$.page'), 1);
    l_size   NUMBER       := NVL(json_value(p_body, '$.pageSize'), 20);
    l_data   CLOB;
    l_meta   CLOB;
    l_total  NUMBER;
  BEGIN
    pkg_esign_session.set_client(p_client_id);

    SELECT COUNT(*) INTO l_total FROM document
     WHERE client_id = p_client_id
       AND (l_env IS NULL OR environment = l_env)
       AND (l_estado IS NULL OR estado = l_estado)
       AND (l_tipo IS NULL OR tipo_de = l_tipo);

    SELECT JSON_ARRAYAGG(
             JSON_OBJECT('cdc' VALUE cdc, 'tipo_de' VALUE tipo_de, 'environment' VALUE environment,
                         'num_documento' VALUE num_documento, 'estado' VALUE estado,
                         'cod_res' VALUE cod_res, 'prot_aut' VALUE prot_aut,
                         'receptor_nombre' VALUE receptor_nombre, 'moneda' VALUE moneda,
                         'total_operacion' VALUE total_operacion,
                         'fecha_emision' VALUE TO_CHAR(fecha_emision, 'YYYY-MM-DD"T"HH24:MI:SSTZH:TZM')
                         RETURNING CLOB) ORDER BY fecha_emision DESC RETURNING CLOB)
      INTO l_data
      FROM (
        SELECT * FROM document
         WHERE client_id = p_client_id
           AND (l_env IS NULL OR environment = l_env)
           AND (l_estado IS NULL OR estado = l_estado)
           AND (l_tipo IS NULL OR tipo_de = l_tipo)
         ORDER BY fecha_emision DESC
         OFFSET (l_page - 1) * l_size ROWS FETCH NEXT l_size ROWS ONLY
      );

    SELECT JSON_OBJECT('page' VALUE l_page, 'pageSize' VALUE l_size, 'total' VALUE l_total RETURNING CLOB)
      INTO l_meta FROM dual;
    p_out := pkg_esign_util.fn_ok_meta(NVL(l_data, TO_CLOB('[]')), l_meta);
  END pr_list_documents;

  PROCEDURE pr_get_document(p_client_id IN NUMBER, p_cdc IN VARCHAR2, p_out OUT CLOB) IS
    l_data CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);
    BEGIN
      SELECT JSON_OBJECT(
               'cdc' VALUE cdc, 'tipo_de' VALUE tipo_de, 'environment' VALUE environment,
               'num_documento' VALUE num_documento, 'establecimiento' VALUE establecimiento,
               'punto_expedicion' VALUE punto_expedicion, 'estado' VALUE estado,
               'cod_res' VALUE cod_res, 'prot_aut' VALUE prot_aut, 'mensaje_res' VALUE mensaje_res,
               'receptor_nombre' VALUE receptor_nombre, 'receptor_doc' VALUE receptor_doc,
               'moneda' VALUE moneda, 'total_operacion' VALUE total_operacion,
               'fecha_emision' VALUE TO_CHAR(fecha_emision, 'YYYY-MM-DD"T"HH24:MI:SSTZH:TZM')
               RETURNING CLOB)
        INTO l_data FROM document WHERE client_id = p_client_id AND cdc = p_cdc;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN raise_application_error(-20404, 'documento inexistente');
    END;
    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_get_document;

  PROCEDURE pr_get_xml(p_client_id IN NUMBER, p_cdc IN VARCHAR2, p_out OUT CLOB) IS
  BEGIN
    pkg_esign_session.set_client(p_client_id);
    BEGIN
      SELECT x.xml_firmado INTO p_out
        FROM document_xml x JOIN document d ON d.id_document = x.document_id
       WHERE d.client_id = p_client_id AND d.cdc = p_cdc;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN raise_application_error(-20404, 'XML inexistente');
    END;
  END pr_get_xml;

END pkg_esign_document_api;
/
