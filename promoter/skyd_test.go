package promoter

import (
	"testing"

	"gitlab.com/NebulousLabs/fastrand"
	"go.sia.tech/siad/types"
)

// TestWatchedSkydAddresses is a unit test for staticWatchedSkydAddresses.
func TestWatchedSkydAddresses(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	p, node, err := newTestPromoter(t.Name(), t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := node.Close(); err != nil {
			t.Fatal(err)
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	var addr1 types.UnlockHash
	fastrand.Read(addr1[:])
	var addr2 types.UnlockHash
	fastrand.Read(addr2[:])
	var addr3 types.UnlockHash
	fastrand.Read(addr3[:])

	// Add addr3 twice.
	addrs := []types.UnlockHash{addr1, addr2, addr3, addr3}
	err = p.staticSkyd.WalletWatchAddPost(addrs, true)
	if err != nil {
		t.Fatal(err)
	}

	// Put them in a map.
	addrsMap := make(map[types.UnlockHash]struct{})
	for _, addr := range addrs {
		addrsMap[addr] = struct{}{}
	}

	// Get addresses.
	skydAddrs, err := p.staticWatchedSkydAddresses()
	if err != nil {
		t.Fatal(err)
	}
	if len(skydAddrs) != len(addrsMap) {
		t.Fatalf("wrong number of addresses %v != %v", len(skydAddrs), len(addrsMap))
	}

	// The right addresses should be returned.
	for _, addr := range skydAddrs {
		_, exists := addrsMap[addr]
		if !exists {
			t.Fatal("addr doesn't exist in map")
		}
	}
}

// TestProcessAddressUpdate is a unit test for managedProcessAddressUpdate.
func TestProcessAddressUpdate(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	p, node, err := newTestPromoter(t.Name(), t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := node.Close(); err != nil {
			t.Fatal(err)
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	var addr1 types.UnlockHash
	fastrand.Read(addr1[:])
	var addr2 types.UnlockHash
	fastrand.Read(addr2[:])

	// Add addr1 twice and remove addr2 even though it doesn't exist.
	// Both of these things shouldn't cause an error and simply result in
	// addr1 being watched.
	addr1Insert := WatchedAddressUpdate{
		Address:       addr1,
		OperationType: operationTypeInsert,
	}
	addr2Delete := WatchedAddressUpdate{
		Address:       addr2,
		OperationType: operationTypeDelete,
	}
	updates := []WatchedAddressUpdate{addr1Insert, addr2Delete, addr1Insert}
	err = p.managedProcessAddressUpdate(true, updates...)
	if err != nil {
		t.Fatal(err)
	}

	// Check skyd.
	wg, err := p.staticSkyd.WalletWatchGet()
	if err != nil {
		t.Fatal(err)
	}
	if len(wg.Addresses) != 1 {
		t.Fatal("wrong length", len(wg.Addresses))
	}
	if wg.Addresses[0] != addr1 {
		t.Fatal("wrong address")
	}

	// Delete addr1 twice.
	addr1Delete := WatchedAddressUpdate{
		Address:       addr1,
		OperationType: operationTypeDelete,
	}
	updates = []WatchedAddressUpdate{addr1Delete, addr1Delete}
	err = p.managedProcessAddressUpdate(true, updates...)
	if err != nil {
		t.Fatal(err)
	}

	// Check skyd again.
	wg, err = p.staticSkyd.WalletWatchGet()
	if err != nil {
		t.Fatal(err)
	}
	if len(wg.Addresses) != 0 {
		t.Fatal("wrong length", len(wg.Addresses))
	}
}
