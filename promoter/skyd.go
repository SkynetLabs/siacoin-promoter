package promoter

import "go.sia.tech/siad/types"

// managedProcessAddressUpdate processes an update reported by
// threadedAddressWatcher by forwarding it to skyd.
func (db *Promoter) managedProcessAddressUpdate(update WatchedAddressUpdate) {
	// TODO: implement
}

// staticWatchedSkydAddresses returns the addresses currently watched by skyd.
func (p *Promoter) staticWatchedSkydAddresses() ([]types.UnlockHash, error) {
	wag, err := p.staticSkyd.WalletWatchGet()
	if err != nil {
		return nil, err
	}
	return wag.Addresses, nil
}
