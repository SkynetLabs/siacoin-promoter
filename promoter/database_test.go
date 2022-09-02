package promoter

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"gitlab.com/NebulousLabs/fastrand"
	"gitlab.com/SkynetLabs/skyd/build"
	"go.sia.tech/siad/types"
)

const (
	testUsername = "admin"
	// nolint:gosec // Disable gosec since these are only test credentials.
	testPassword = "aO4tV5tC1oU3oQ7u"
	testURI      = "mongodb://localhost:37017"
)

// TestAddressWatcher is a unit test for threadedAddressWatcher.
func TestAddressWatcher(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	inserted := make(map[types.UnlockHash]struct{})
	deleted := make(map[types.UnlockHash]struct{})
	var mu sync.Mutex
	f := func(update WatchedAddressUpdate) {
		mu.Lock()
		defer mu.Unlock()
		switch update.OperationType {
		case operationTypeInsert:
			inserted[update.Address] = struct{}{}
		case operationTypeDelete:
			deleted[update.Address] = struct{}{}
		default:
		}
	}

	p, node, err := newTestPromoterWithUpdateFunc(t.Name(), f)
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

	// Reset database for the test.
	if err := p.staticDB.Drop(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Add some addresses.
	var addrs []types.UnlockHash
	for i := 0; i < 3; i++ {
		var addr types.UnlockHash
		fastrand.Read(addr[:])
		addrs = append(addrs, addr)

		if err := p.Watch(context.Background(), addr); err != nil {
			t.Fatal(err)
		}
	}

	// Check if all addresses have been added.
	err = build.Retry(100, 100*time.Millisecond, func() error {
		mu.Lock()
		defer mu.Unlock()
		if len(inserted) != len(addrs) {
			return fmt.Errorf("not all addresses were inserted %v != %v", len(inserted), len(addrs))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Remove them again.
	for _, addr := range addrs {
		if err := p.Unwatch(context.Background(), addr); err != nil {
			t.Fatal(err)
		}
	}

	// Check if all addresses have been added once and deleted once.
	err = build.Retry(100, 100*time.Millisecond, func() error {
		mu.Lock()
		defer mu.Unlock()
		// Check that the callback was called the right number of times and with the
		// right addresses.
		if len(inserted) != len(addrs) || len(deleted) != len(addrs) {
			return fmt.Errorf("%v != %v != %v", len(inserted), len(addrs), len(deleted))
		}
		for _, addr := range addrs {
			_, exists := inserted[addr]
			if !exists {
				return fmt.Errorf("addr %v missing in inserted", addr)
			}
			_, exists = deleted[addr]
			if !exists {
				return fmt.Errorf("addr %v missing in deleted", addr)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Add them back.
	for _, addr := range addrs {
		if err := p.Watch(context.Background(), addr); err != nil {
			t.Fatal(err)
		}
	}

	// Prepare a new node that connects to the same db.
	inserted2 := make(map[types.UnlockHash]struct{})
	deleted2 := make(map[types.UnlockHash]struct{})
	f2 := func(update WatchedAddressUpdate) {
		mu.Lock()
		defer mu.Unlock()
		switch update.OperationType {
		case operationTypeInsert:
			inserted2[update.Address] = struct{}{}
		case operationTypeDelete:
			deleted2[update.Address] = struct{}{}
		default:
		}
	}
	p2, node2, err := newTestPromoterWithUpdateFunc(t.Name()+"2", f2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := node2.Close(); err != nil {
			t.Fatal(err)
		}
		if err := p2.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// All addresses should be added on startup.
	err = build.Retry(100, 100*time.Millisecond, func() error {
		mu.Lock()
		defer mu.Unlock()
		// Check that the callback was called the right number of times and with the
		// right addresses.
		if len(inserted2) != len(addrs) || len(deleted2) != 0 {
			return fmt.Errorf("should have %v inserted (got %v) but 0 deleted (got %v)", len(addrs), len(inserted2), len(deleted2))
		}
		for _, addr := range addrs {
			_, exists := inserted2[addr]
			if !exists {
				return fmt.Errorf("addr %v missing in inserted", addr)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

}

// TestWatchedDBAddresses is a unit test or staticWatchedDBAddresses.
func TestWatchedDBAddresses(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

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

	// Reset database for the test.
	if err := p.staticDB.Drop(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Add some addresses.
	var addr1 types.UnlockHash
	fastrand.Read(addr1[:])
	var addr2 types.UnlockHash
	fastrand.Read(addr2[:])
	var addr3 types.UnlockHash
	fastrand.Read(addr3[:])

	// Add addr3 twice.
	addrs := []types.UnlockHash{addr1, addr2, addr3, addr3}

	// Call Watch to add them.
	for _, addr := range addrs {
		if err := p.Watch(context.Background(), addr); err != nil {
			t.Fatal(err)
		}
	}

	// Get addresses from db.
	addrsMap := make(map[types.UnlockHash]struct{})
	for _, addr := range addrs {
		addrsMap[addr] = struct{}{}
	}

	dbAddrs, err := p.staticWatchedDBAddresses(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Check addresses.
	if len(dbAddrs) != len(addrsMap) {
		t.Fatalf("wrong number of addrs %v != %v", len(dbAddrs), len(addrsMap))
	}
	for _, addr := range dbAddrs {
		_, exists := addrsMap[addr]
		if !exists {
			t.Fatal("addr doesn't exist")
		}
	}
}
