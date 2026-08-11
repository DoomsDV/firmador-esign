-- PKG_ESIGN_KUDE_API: branding del KuDE (plantilla/color/logo/footer) por cliente,
-- subida del logo y del PDF final al bucket OCI (via PKG_ESIGN_BUCKET, sin bucket
-- nuevo) y lectura del kude_url ya generado. El binario (logo/PDF) llega en hex
-- (mismo patron que csc_ciphertext/p12_ciphertext: pkg_esign_util.fn_hex_to_blob).
-- Escritura de config/logo restringida a 'owner'; pr_store_kude es interno (Go).
CREATE OR REPLACE PACKAGE pkg_esign_kude_api AS

  -- Config JSON cruda (sin envelope), para insertar en otras respuestas (contexto interno).
  FUNCTION fn_config_json(p_client_id IN NUMBER) RETURN CLOB;

  -- Panel: lectura/escritura del branding.
  PROCEDURE pr_get_config(p_client_id IN NUMBER, p_out OUT CLOB);
  PROCEDURE pr_upsert_config(p_client_id IN NUMBER, p_role IN VARCHAR2, p_body IN CLOB, p_out OUT CLOB);

  -- Panel: sube el logo (imagen) al bucket y actualiza logo_url. p_image_hex es la
  -- imagen en hex; p_mime_type determina la extension del objeto.
  PROCEDURE pr_upload_logo(
    p_client_id  IN NUMBER,
    p_role       IN VARCHAR2,
    p_image_hex  IN CLOB,
    p_mime_type  IN VARCHAR2,
    p_out        OUT CLOB
  );

  -- Interno (Go): sube el PDF ya renderizado (Gotenberg) y actualiza document_xml.kude_url.
  PROCEDURE pr_store_kude(
    p_client_id IN NUMBER,
    p_cdc       IN VARCHAR2,
    p_pdf_hex   IN CLOB,
    p_out       OUT CLOB
  );

  -- Panel: URL del KuDE de un documento (o "pending" si aun no se genero).
  PROCEDURE pr_get_kude(p_client_id IN NUMBER, p_cdc IN VARCHAR2, p_out OUT CLOB);

END pkg_esign_kude_api;
/

CREATE OR REPLACE PACKAGE BODY pkg_esign_kude_api AS

  c_default_template CONSTANT VARCHAR2(30) := 'minimalista';
  c_default_color    CONSTANT VARCHAR2(7)  := '#0f172a';

  PROCEDURE assert_owner(p_role IN VARCHAR2) IS
  BEGIN
    IF p_role <> 'owner' THEN
      raise_application_error(pkg_esign_http.c_ora_forbidden, 'solo el owner puede modificar el diseno del KuDE');
    END IF;
  END assert_owner;

  -- ext_for_mime mapea el content-type del logo a una extension de archivo simple.
  FUNCTION ext_for_mime(p_mime IN VARCHAR2) RETURN VARCHAR2 IS
  BEGIN
    CASE LOWER(NVL(p_mime, ''))
      WHEN 'image/png'     THEN RETURN 'png';
      WHEN 'image/jpeg'     THEN RETURN 'jpg';
      WHEN 'image/jpg'      THEN RETURN 'jpg';
      WHEN 'image/svg+xml'  THEN RETURN 'svg';
      WHEN 'image/webp'     THEN RETURN 'webp';
      ELSE RETURN 'png';
    END CASE;
  END ext_for_mime;

  FUNCTION fn_config_json(p_client_id IN NUMBER) RETURN CLOB IS
    l_data CLOB;
  BEGIN
    SELECT JSON_OBJECT(
             'template_id'      VALUE NVL(k.template_id, c_default_template),
             'color_primario'   VALUE NVL(k.color_primario, c_default_color),
             'logo_url'         VALUE k.logo_url,
             'notas_footer'     VALUE k.notas_footer,
             'mostrar_fantasia' VALUE NVL(k.mostrar_fantasia, 1)
             RETURNING CLOB)
      INTO l_data
      FROM dual
      LEFT JOIN client_kude_config k ON k.client_id = p_client_id;
    RETURN l_data;
  END fn_config_json;

  PROCEDURE pr_get_config(p_client_id IN NUMBER, p_out OUT CLOB) IS
  BEGIN
    pkg_esign_session.set_client(p_client_id);
    p_out := pkg_esign_util.fn_ok(fn_config_json(p_client_id));
  END pr_get_config;

  PROCEDURE pr_upsert_config(p_client_id IN NUMBER, p_role IN VARCHAR2, p_body IN CLOB, p_out OUT CLOB) IS
    l_template  VARCHAR2(30) := json_value(p_body, '$.template_id');
    l_mostrar   NUMBER       := json_value(p_body, '$.mostrar_fantasia');
  BEGIN
    assert_owner(p_role);
    pkg_esign_session.set_client(p_client_id);

    IF l_template IS NOT NULL AND l_template NOT IN ('minimalista', 'corporativa') THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request, 'template_id debe ser minimalista o corporativa');
    END IF;

    IF l_mostrar IS NOT NULL AND l_mostrar NOT IN (0, 1) THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request, 'mostrar_fantasia debe ser 0 o 1');
    END IF;

    MERGE INTO client_kude_config k
    USING (SELECT p_client_id AS cid FROM dual) s
    ON (k.client_id = s.cid)
    WHEN MATCHED THEN UPDATE SET
      template_id      = NVL(l_template, k.template_id),
      color_primario   = NVL(json_value(p_body, '$.color_primario'), k.color_primario),
      notas_footer     = json_value(p_body, '$.notas_footer'),
      mostrar_fantasia = NVL(l_mostrar, k.mostrar_fantasia),
      updated_at       = CURRENT_TIMESTAMP
    WHEN NOT MATCHED THEN INSERT (client_id, template_id, color_primario, notas_footer, mostrar_fantasia)
    VALUES (p_client_id, NVL(l_template, c_default_template),
            NVL(json_value(p_body, '$.color_primario'), c_default_color),
            json_value(p_body, '$.notas_footer'),
            NVL(l_mostrar, 1));

    p_out := pkg_esign_util.fn_ok(fn_config_json(p_client_id));
  END pr_upsert_config;

  PROCEDURE pr_upload_logo(
    p_client_id  IN NUMBER,
    p_role       IN VARCHAR2,
    p_image_hex  IN CLOB,
    p_mime_type  IN VARCHAR2,
    p_out        OUT CLOB
  ) IS
    l_blob   BLOB;
    l_url    VARCHAR2(2000);
    l_status NUMBER;
    l_data   CLOB;
  BEGIN
    assert_owner(p_role);
    pkg_esign_session.set_client(p_client_id);

    IF p_image_hex IS NULL OR dbms_lob.getlength(p_image_hex) = 0 THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request, 'image_hex requerido');
    END IF;
    l_blob := pkg_esign_util.fn_hex_to_blob(p_image_hex);

    pkg_esign_bucket.pr_put_object(
      p_object_name => 'clients/' || p_client_id || '/branding/logo.' || ext_for_mime(p_mime_type),
      p_blob        => l_blob,
      p_mime_type   => NVL(p_mime_type, 'application/octet-stream'),
      po_url        => l_url,
      po_status     => l_status
    );

    MERGE INTO client_kude_config k
    USING (SELECT p_client_id AS cid FROM dual) s
    ON (k.client_id = s.cid)
    WHEN MATCHED THEN UPDATE SET logo_url = l_url, updated_at = CURRENT_TIMESTAMP
    WHEN NOT MATCHED THEN INSERT (client_id, template_id, color_primario, logo_url)
    VALUES (p_client_id, c_default_template, c_default_color, l_url);

    SELECT JSON_OBJECT('logo_url' VALUE l_url RETURNING CLOB) INTO l_data FROM dual;
    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_upload_logo;

  PROCEDURE pr_store_kude(
    p_client_id IN NUMBER,
    p_cdc       IN VARCHAR2,
    p_pdf_hex   IN CLOB,
    p_out       OUT CLOB
  ) IS
    l_blob   BLOB;
    l_env    document.environment%TYPE;
    l_doc_id document.id_document%TYPE;
    l_url    VARCHAR2(2000);
    l_status NUMBER;
    l_data   CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);

    IF p_pdf_hex IS NULL OR dbms_lob.getlength(p_pdf_hex) = 0 THEN
      raise_application_error(pkg_esign_http.c_ora_bad_request, 'pdf_hex requerido');
    END IF;

    BEGIN
      SELECT id_document, environment INTO l_doc_id, l_env
        FROM document WHERE client_id = p_client_id AND cdc = p_cdc;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN
        raise_application_error(pkg_esign_http.c_ora_not_found, 'documento inexistente para ese cdc');
    END;

    l_blob := pkg_esign_util.fn_hex_to_blob(p_pdf_hex);

    pkg_esign_bucket.pr_put_object(
      p_object_name => 'kude/' || LOWER(l_env) || '/' || p_client_id || '/' || p_cdc || '.pdf',
      p_blob        => l_blob,
      p_mime_type   => 'application/pdf',
      po_url        => l_url,
      po_status     => l_status
    );

    MERGE INTO document_xml x
    USING (SELECT l_doc_id AS did FROM dual) s
    ON (x.document_id = s.did)
    WHEN MATCHED THEN UPDATE SET kude_url = l_url
    WHEN NOT MATCHED THEN INSERT (document_id, client_id, kude_url)
    VALUES (l_doc_id, p_client_id, l_url);

    SELECT JSON_OBJECT('kude_url' VALUE l_url RETURNING CLOB) INTO l_data FROM dual;
    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_store_kude;

  PROCEDURE pr_get_kude(p_client_id IN NUMBER, p_cdc IN VARCHAR2, p_out OUT CLOB) IS
    l_url  VARCHAR2(1000);
    l_data CLOB;
  BEGIN
    pkg_esign_session.set_client(p_client_id);
    BEGIN
      SELECT x.kude_url INTO l_url
        FROM document_xml x JOIN document d ON d.id_document = x.document_id
       WHERE d.client_id = p_client_id AND d.cdc = p_cdc;
    EXCEPTION
      WHEN NO_DATA_FOUND THEN
        raise_application_error(pkg_esign_http.c_ora_not_found, 'documento inexistente para ese cdc');
    END;

    SELECT JSON_OBJECT(
             'kude_url' VALUE l_url,
             'estado'   VALUE CASE WHEN l_url IS NULL THEN 'pending' ELSE 'ready' END
             RETURNING CLOB)
      INTO l_data FROM dual;
    p_out := pkg_esign_util.fn_ok(l_data);
  END pr_get_kude;

END pkg_esign_kude_api;
/
