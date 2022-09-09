package promoter

import (
	"fmt"

	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/SkynetLabs/skyd/node/api/client"
	"go.sia.tech/siad/node/api"
	"go.sia.tech/siad/types"
)

// managedProcessAddressUpdate processes an update reported by
// threadedAddressWatcher by forwarding it to skyd.
// 'unused' specifies whether the inserted update is expected to contain an
// unused address. This only affects additions however since we can't make that
// assumption about removals.
func (p *Promoter) managedProcessAddressUpdate(unused bool, updates ...WatchedAddressUpdate) error {
	// If there are not updates there is nothing to do.
	if len(updates) == 0 {
		return nil
	}
	// Deduplicate updates to make sure we only have the latest update for
	// each address.
	uniqueUpdates := make(map[types.UnlockHash]WatchedAddressUpdate)
	for _, update := range updates {
		uniqueUpdates[update.Address] = update
	}
	// Sort the updates into additions and removals.
	var additions, removals []types.UnlockHash
	for _, update := range uniqueUpdates {
		switch update.OperationType {
		case operationTypeInsert:
			additions = append(additions, update.Address)
		case operationTypeDelete:
			removals = append(removals, update.Address)
		default:
			// Ignore the remaining updates.
		}
	}
	// Remove addresses from skyd first. We always use 'unused' == true
	// here even if the address wasn't unused to avoid a resync of the
	// wallet for deletions. That's because for deletions we aren't afraid
	// about missing past txns.
	if err := p.staticSkyd.WalletWatchRemovePost(removals, true); err != nil {
		return errors.AddContext(err, "failed to remove addresses from skyd")
	}
	if err := p.staticSkyd.WalletWatchAddPost(additions, unused); err != nil {
		return errors.AddContext(err, "failed to add addresses to skyd")
	}
	return nil
}

// staticWatchedSkydAddresses returns the addresses currently watched by skyd.
func (p *Promoter) staticWatchedSkydAddresses() ([]types.UnlockHash, error) {
	wag, err := p.staticSkyd.WalletWatchGet()
	if err != nil {
		return nil, err
	}
	return wag.Addresses, nil
}

// staticTxnsByAddress fetches all confirmed transactions for a given address
// from skyd and returns them as an interface slice ready to be inserted into
// the database.
func (p *Promoter) staticTxnsByAddress(addr types.UnlockHash) ([]interface{}, error) {
	// Need to use the unsafe client since there is no safe method for that
	// endpoint.
	c := client.NewUnsafeClient(*p.staticSkyd)

	// Get txns related to the provided address.
	var wtag api.WalletTransactionsGETaddr
	err := c.Get(fmt.Sprintf("/wallet/transactions/%s", addr), &wtag)
	if err != nil {
		return nil, err
	}

	// Go through all the related confirmed transactions and find the ones
	// for which the address is an output a.k.a. the receiver of the funds.
	// Then sum up the received funds through that transaction and append it
	// to the slice we return.
	var txns []interface{}
	for _, txn := range wtag.ConfirmedTransactions {
		save := false
		var value types.Currency
		for _, out := range txn.Outputs {
			if out.RelatedAddress == addr {
				value = value.Add(out.Value)
				save = true
			}
		}
		if save {
			txns = append(txns, Transaction{
				Address: addr,
				TxnID:   txn.TransactionID,
				Value:   value.String(),
			})
		}
	}
	return txns, nil
}
