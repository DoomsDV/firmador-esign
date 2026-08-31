package sifen

import (
	"strings"
	"testing"
)

func TestParseRDEXMLMinimal(t *testing.T) {
	t.Parallel()

	raw := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<rDE xmlns="http://ekuatia.set.gov.py/sifen/xsd">
  <dVerFor>150</dVerFor>
  <DE Id="01800603864001001000000122026073111234567890">
    <dDVId>1</dDVId>
    <dFecFirma>2026-07-31T12:00:00</dFecFirma>
    <dSisFact>1</dSisFact>
    <gOpeDE><iTipEmi>1</iTipEmi><dDesTipEmi>Normal</dDesTipEmi><dCodSeg>123456789</dCodSeg></gOpeDE>
    <gTimb>
      <iTiDE>1</iTiDE><dDesTiDE>Factura electrónica</dDesTiDE>
      <dNumTim>06038964</dNumTim><dEst>001</dEst><dPunExp>001</dPunExp>
      <dNumDoc>0000001</dNumDoc><dFeIniT>2026-07-09</dFeIniT>
    </gTimb>
    <gDatGralOpe>
      <dFeEmiDE>2026-07-31T12:00:00</dFeEmiDE>
      <gEmis>
        <dRucEm>6038964</dRucEm><dDVEmi>8</dDVEmi><iTipCont>1</iTipCont>
        <dNomEmi>TEST</dNomEmi><dDirEmi>Calle</dDirEmi><dNumCas>0</dNumCas>
        <cDepEmi>12</cDepEmi><dDesDepEmi>CENTRAL</dDesDepEmi>
        <cDisEmi>153</cDisEmi><dDesDisEmi>CAPIATA</dDesDisEmi>
        <cCiuEmi>3568</cCiuEmi><dDesCiuEmi>CAPIATA</dDesCiuEmi>
      </gEmis>
      <gDatRec>
        <iNatRec>1</iNatRec><iTiOpe>2</iTiOpe><cPaisRec>PRY</cPaisRec>
        <dNomRec>Cliente</dNomRec>
      </gDatRec>
    </gDatGralOpe>
    <gDtipDE/>
  </DE>
</rDE>`)

	rde, err := ParseRDEXML(raw)
	if err != nil {
		t.Fatalf("ParseRDEXML: %v", err)
	}
	if rde.DE == nil || !strings.HasPrefix(rde.DE.Id, "0180") {
		t.Fatalf("CDC = %v", rde.DE)
	}
	if rde.DE.GTimb.DNumTim != "06038964" {
		t.Fatalf("timbrado = %q", rde.DE.GTimb.DNumTim)
	}
}

func TestParseRDEXMLEmpty(t *testing.T) {
	t.Parallel()
	if _, err := ParseRDEXML(nil); err == nil {
		t.Fatal("expected error")
	}
}
