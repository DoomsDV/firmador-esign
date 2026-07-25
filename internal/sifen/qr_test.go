package sifen

import "testing"

func TestHashQRManualTecnico1384(t *testing.T) {
	// Ejemplo MT 13.8.4 (hash independiente de la base URL)
	p := QRParams{
		CDC:         "01444444017001001001452822017012515873260988",
		FeEmiDE:     "2017-01-25T09:35:17",
		RecID:       "88899990",
		TotGralOpe:  "300000",
		TotIVA:      "27272",
		Items:       2,
		DigestValue: "yzGYhUx1/XYYzksWB+fPR3Qc50c=",
		IdCSC:       "0001",
		CSC:         "ABCD0000000000000000000000000000",
		QRBase:      QRBaseURLTest,
	}

	query := BuildQueryString(p)
	wantQueryPrefix := "nVersion=150&Id=01444444017001001001452822017012515873260988&dFeEmiDE=323031372d30312d32355430393a33353a3137&dRucRec=88899990&dTotGralOpe=300000&dTotIVA=27272&cItems=2&DigestValue=797a4759685578312f5859597a6b7357422b6650523351633530633d&IdCSC=0001"
	if query != wantQueryPrefix {
		t.Fatalf("query mismatch\ngot:  %s\nwant: %s", query, wantQueryPrefix)
	}

	got := HashQR(query, p.CSC)
	want := "97ddbb3c1e7d65af03a70ffe21f2b34846ab1c89e0566c35222086766b7374ed"
	if got != want {
		t.Fatalf("cHashQR mismatch\ngot:  %s\nwant: %s", got, want)
	}

	url, err := BuildCarQR(p)
	if err != nil {
		t.Fatal(err)
	}
	wantURL := QRBaseURLTest + wantQueryPrefix + "&cHashQR=" + want
	if url != wantURL {
		t.Fatalf("URL mismatch\ngot:  %s\nwant: %s", url, wantURL)
	}
}
