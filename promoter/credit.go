package promoter

import (
	"fmt"
	"math/big"

	"go.sia.tech/siad/types"
)

// creditPrecision is the precision of the credits when sending them to the
// credit service. We use a generous value here to not lose too much precision.
const creditPrecision = 20

// convertSCToCredits converts a given amount of siacoin to credits using the
// provided conversion rate.
func convertSCToCredits(sc types.Currency, conversionRate *big.Rat) *big.Rat {
	scRat := new(big.Rat).SetFrac(sc.Big(), big.NewInt(1))
	return scRat.Mul(scRat, conversionRate)
}

// staticCreditTxn credits a txn with a given id and amount to the creditor for
// the user. This includes taking a txn's Siacoin value, converting it to an
// amount of credits and then calling the creditor with that amount.
func (p *Promoter) staticCreditTxn(userSub string, txnID types.TransactionID, amt types.Currency, cr *big.Rat) error {
	// Convert the amount.
	credits := convertSCToCredits(amt, cr)

	// Convert credits to a string.
	creditsStr := credits.FloatString(creditPrecision)

	// TODO: send request.
	fmt.Println("creditsStr", creditsStr)

	return nil
}
