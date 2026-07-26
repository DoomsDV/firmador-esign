-- document_xml: XML firmado + QR + KuDE de cada documento (1:1 con document).
-- Se guarda EXACTAMENTE el DOM firmado que se envio a SIFEN (regla 0141: no re-marshalizar).
CREATE TABLE document_xml (
  document_id NUMBER PRIMARY KEY,
  client_id   NUMBER NOT NULL,                         -- desnormalizado para la VPD
  xml_firmado CLOB,
  qr_url      VARCHAR2(1000),
  kude_blob   BLOB,
  CONSTRAINT fk_document_xml_doc    FOREIGN KEY (document_id) REFERENCES document (id_document) ON DELETE CASCADE,
  CONSTRAINT fk_document_xml_client FOREIGN KEY (client_id)   REFERENCES client (id_client) ON DELETE CASCADE
);

COMMENT ON TABLE document_xml IS 'XML firmado (tal cual se envio a SIFEN), QR y KuDE. 1:1 con document.';
