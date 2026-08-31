-- document_xml: XML firmado + QR + KuDE de cada documento (1:1 con document).
-- Se guarda EXACTAMENTE el DOM firmado que se envio a SIFEN (regla 0141: no re-marshalizar).
-- Metadatos del XML (sha256/size/mime/captured/availability) permiten verificacion sin
-- republicar el CLOB; el XML NUNCA se sube a Object Storage.
CREATE TABLE document_xml (
  document_id      NUMBER PRIMARY KEY,
  client_id        NUMBER NOT NULL,                         -- desnormalizado para la VPD
  xml_firmado      CLOB,
  xml_sha256       VARCHAR2(64),                            -- hex lowercase SHA-256 de los bytes UTF-8
  xml_size_bytes   NUMBER,
  xml_mime_type    VARCHAR2(100) DEFAULT 'application/xml; charset=UTF-8',
  xml_captured_at  TIMESTAMP(6) WITH TIME ZONE,
  xml_availability VARCHAR2(20) DEFAULT 'MISSING',          -- MISSING|AVAILABLE|CORRUPT
  qr_url           VARCHAR2(1000),
  kude_blob        BLOB,                                   -- sin uso (ver kude_url); reservado
  kude_url         VARCHAR2(1000),                         -- URL publica del KuDE en bucket-etick-dev
  CONSTRAINT fk_document_xml_doc    FOREIGN KEY (document_id) REFERENCES document (id_document) ON DELETE CASCADE,
  CONSTRAINT fk_document_xml_client FOREIGN KEY (client_id)   REFERENCES client (id_client) ON DELETE CASCADE,
  CONSTRAINT ck_document_xml_avail  CHECK (xml_availability IN ('MISSING', 'AVAILABLE', 'CORRUPT'))
);

COMMENT ON TABLE document_xml IS 'XML firmado (tal cual se envio a SIFEN), metadatos verificables, QR y KuDE. 1:1 con document.';
COMMENT ON COLUMN document_xml.xml_sha256 IS 'SHA-256 hex del XML firmado canónico (bytes UTF-8).';
COMMENT ON COLUMN document_xml.xml_availability IS 'MISSING=sin CLOB; AVAILABLE=CLOB+hash; CORRUPT=inconsistente.';
