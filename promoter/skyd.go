package promoter

import (
	"gitlab.com/NebulousLabs/errors"
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
