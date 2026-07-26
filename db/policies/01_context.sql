-- ESIGN_CTX: application context del tenant activo. Solo pkg_esign_session puede escribirlo.
-- La VPD lee sys_context('ESIGN_CTX','CLIENT_ID') para filtrar por client.
CREATE OR REPLACE CONTEXT esign_ctx USING pkg_esign_session;
