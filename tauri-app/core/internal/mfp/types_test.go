package mfp

import (
	"math"
	"testing"
)

func TestEnergyKcal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   *Energy
		want *float64
	}{
		{"EP-173 calories are already kcal", &Energy{Unit: "calories", Value: 140}, f(140)},
		{"EP-174 kilojoules are divided by 4.184", &Energy{Unit: "kilojoules", Value: 4184}, f(1000)},
		{"EP-175 a missing energy stays missing", nil, nil},
		{"EP-176 a unit other than kilojoules is read as kcal", &Energy{Unit: "", Value: 99}, f(99)},
		{"BVA-122 zero kilojoules is zero kcal, not missing", &Energy{Unit: "kilojoules", Value: 0}, f(0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.in.Kcal()
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("kcal = %v, want nil", *got)
			case tc.want != nil && (got == nil || math.Abs(*got-*tc.want) > 1e-6):
				t.Fatalf("kcal = %v, want %v", got, *tc.want)
			}
		})
	}
}
