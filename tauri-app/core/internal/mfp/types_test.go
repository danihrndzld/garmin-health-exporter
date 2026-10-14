package mfp

import "testing"

func TestEnergyKcal(t *testing.T) {
	k := (&Energy{Unit: "calories", Value: 140}).Kcal()
	if k == nil || *k != 140 {
		t.Fatalf("calories kcal = %v", k)
	}
	if (*Energy)(nil).Kcal() != nil {
		t.Fatal("nil energy must give nil kcal")
	}
}
