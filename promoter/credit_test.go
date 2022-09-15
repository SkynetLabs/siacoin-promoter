package promoter

import (
	"math/big"
	"testing"

	"go.sia.tech/siad/types"
)

// TestConvertSCToCredits is a unit test for convertSCToCredits.
func TestConvertSCToCredits(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	tests := []struct {
		sc             types.Currency
		conversionRate *big.Rat
		result         string
	}{
		{
			sc:             types.SiacoinPrecision,
			conversionRate: defaultConversionRate,
			result:         "1.00",
		},
		{
			sc:             types.SiacoinPrecision.Div64(2),
			conversionRate: defaultConversionRate,
			result:         "0.50",
		},
		{
			sc:             types.SiacoinPrecision.Mul64(2),
			conversionRate: defaultConversionRate,
			result:         "2.00",
		},
	}
	for i, test := range tests {
		result := convertSCToCredits(test.sc, test.conversionRate)
		decStr := result.FloatString(2)
		if decStr != test.result {
			t.Fatalf("%v: %v != %v", i, decStr, test.result)
		}
	}
}
