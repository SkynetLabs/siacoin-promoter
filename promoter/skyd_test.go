package promoter

import (
	"testing"

	"gitlab.com/NebulousLabs/fastrand"
	"go.sia.tech/siad/types"
)

// TestWatchedSkydAddresses is a unit test for staticWatchedSkydAddresses.
func TestWatchedSkydAddresses(t *testing.T) {
	p, node, err := newTestPromoter(t.Name())
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
