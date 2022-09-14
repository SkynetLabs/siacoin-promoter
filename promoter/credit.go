package promoter

import (
	"go.sia.tech/siad/types"
)

// staticCreditTxn credits a txn with a given id and amount to the creditor for
// the user. This includes taking a txn's Siacoin value, converting it to an
// amount of credits and then calling the creditor with that amount.
func (p *Promoter) staticCreditTxn(userSub string, txnID types.TransactionID, amt types.Currency) error {
	// TODO: Implement once we have a client lib for the credit system.
	return nil
}
