package sifen

import (
	"testing"

	"github.com/shopspring/decimal"
)

func di(n int64) decimal.Decimal { return decimal.NewFromInt(n) }

func TestBuildItem_IVA(t *testing.T) {
	tests := []struct {
		name     string
		in       ItemInput
		moneda   string
		wantBase int64
		wantIVA  int64
		wantExe  int64
		wantErr  bool
	}{
		{
			name:     "gravado 10",
			in:       ItemInput{Codigo: "A", Descripcion: "X", Cantidad: di(1), PrecioUnitario: di(150000), AfectacionIVA: AfecIVAGravado, TasaIVA: 10},
			moneda:   "PYG",
			wantBase: 136364, wantIVA: 13636, wantExe: 0,
		},
		{
			name:     "gravado 5",
			in:       ItemInput{Codigo: "B", Descripcion: "X", Cantidad: di(1), PrecioUnitario: di(105000), AfectacionIVA: AfecIVAGravado, TasaIVA: 5},
			moneda:   "PYG",
			wantBase: 100000, wantIVA: 5000, wantExe: 0,
		},
		{
			name:     "exento",
			in:       ItemInput{Codigo: "C", Descripcion: "X", Cantidad: di(1), PrecioUnitario: di(50000), AfectacionIVA: AfecIVAExento, TasaIVA: 0},
			moneda:   "PYG",
			wantBase: 0, wantIVA: 0, wantExe: 0, // dBasExe (E737) = 0 para E731=3 (NT13)
		},
		{
			name:     "exonerado",
			in:       ItemInput{Codigo: "D", Descripcion: "X", Cantidad: di(1), PrecioUnitario: di(40000), AfectacionIVA: AfecIVAExonerado, TasaIVA: 0},
			moneda:   "PYG",
			wantBase: 0, wantIVA: 0, wantExe: 0,
		},
		{
			name:     "gravado parcial 10 al 50%",
			in:       ItemInput{Codigo: "E", Descripcion: "X", Cantidad: di(1), PrecioUnitario: di(100000), AfectacionIVA: AfecIVAParcial, TasaIVA: 10, PropIVA: di(50)},
			moneda:   "PYG",
			wantBase: 47619, wantIVA: 4762, wantExe: 47619, // NT13: denom=10000+500=10500
		},
		{
			name:    "gravado sin tasa válida",
			in:      ItemInput{Codigo: "F", Descripcion: "X", Cantidad: di(1), PrecioUnitario: di(1000), AfectacionIVA: AfecIVAGravado, TasaIVA: 0},
			moneda:  "PYG",
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			item, err := BuildItem(tc.in, tc.moneda)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("esperaba error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildItem: %v", err)
			}
			if got := item.GCamIVA.DBasGravIVA.IntPart(); got != tc.wantBase {
				t.Errorf("base gravada: got %d want %d", got, tc.wantBase)
			}
			if got := item.GCamIVA.DLiqIVAItem.IntPart(); got != tc.wantIVA {
				t.Errorf("IVA liquidado: got %d want %d", got, tc.wantIVA)
			}
			if got := item.GCamIVA.DBasExe.IntPart(); got != tc.wantExe {
				t.Errorf("base exenta: got %d want %d", got, tc.wantExe)
			}
			// Invariante: para gravado (total o parcial), base + IVA + exenta =
			// total de operación. Para exento/exonerado los tres campos son 0
			// (el EA008 se refleja solo en el subtotal, no en dBasExe).
			if tc.in.AfectacionIVA == AfecIVAGravado || tc.in.AfectacionIVA == AfecIVAParcial {
				suma := item.GCamIVA.DBasGravIVA.Add(item.GCamIVA.DLiqIVAItem).Add(item.GCamIVA.DBasExe)
				if !suma.Equal(item.GValorItem.GValorRestaItem.DTotOpeItem) {
					t.Errorf("base+IVA+exenta=%s ≠ totOpe=%s", suma, item.GValorItem.GValorRestaItem.DTotOpeItem)
				}
			}
		})
	}
}

func TestCalcularTotales_Mixto(t *testing.T) {
	items := make([]GCamItem, 0, 3)
	for _, in := range []ItemInput{
		{Codigo: "A", Descripcion: "10", Cantidad: di(1), PrecioUnitario: di(110000), AfectacionIVA: AfecIVAGravado, TasaIVA: 10},
		{Codigo: "B", Descripcion: "5", Cantidad: di(1), PrecioUnitario: di(105000), AfectacionIVA: AfecIVAGravado, TasaIVA: 5},
		{Codigo: "C", Descripcion: "exe", Cantidad: di(1), PrecioUnitario: di(50000), AfectacionIVA: AfecIVAExento, TasaIVA: 0},
	} {
		it, err := BuildItem(in, "PYG")
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, it)
	}

	tot := CalcularTotales(items, "PYG")

	assertDec(t, "dSub10", tot.DSub10, 110000)
	assertDec(t, "dSub5", tot.DSub5, 105000)
	assertDec(t, "dSubExe", tot.DSubExe, 50000)
	assertDec(t, "dIVA10", tot.DTotIVA10, 10000)
	assertDec(t, "dIVA5", tot.DTotIVA5, 5000)
	assertDec(t, "dTotIVA", tot.DTotIVA, 15000)
	assertDec(t, "dBaseGrav10", tot.DBaseGrav10, 100000)
	assertDec(t, "dBaseGrav5", tot.DBaseGrav5, 100000)
	assertDec(t, "dTBasGraIVA", tot.DTBasGraIVA, 200000)
	assertDec(t, "dTotOpe", tot.DTotOpe, 265000)
	assertDec(t, "dTotGralOpe", tot.DTotGralOpe, 265000)
}

func assertDec(t *testing.T, name string, got decimal.Decimal, want int64) {
	t.Helper()
	if got.IntPart() != want {
		t.Errorf("%s: got %s want %d", name, got, want)
	}
}

func BenchmarkCalcularTotales(b *testing.B) {
	items := make([]GCamItem, 0, 100)
	for i := 0; i < 100; i++ {
		it, _ := BuildItem(ItemInput{Codigo: "A", Descripcion: "X", Cantidad: di(1), PrecioUnitario: di(150000), AfectacionIVA: AfecIVAGravado, TasaIVA: 10}, "PYG")
		items = append(items, it)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CalcularTotales(items, "PYG")
	}
}
