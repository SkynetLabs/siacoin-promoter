package promoter

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/fastrand"
	"gitlab.com/SkynetLabs/skyd/build"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.sia.tech/siad/types"
)

const (
	testUsername = "admin"
	// nolint:gosec // Disable gosec since these are only test credentials.
	testPassword = "aO4tV5tC1oU3oQ7u"
	testURI      = "mongodb://localhost:37017"
)

// Watch watches an address by adding it to the database.
// threadedAddressWatcher will then pick up on that change and apply it to skyd.
func (p *Promoter) Watch(ctx context.Context, addr types.UnlockHash) error {
	_, err := p.staticColWatchedAddresses().InsertOne(ctx, p.newUnusedWatchedAddress(addr))
	if mongo.IsDuplicateKeyError(err) {
		// nothing to do, the ChangeStream should've picked up on that
		// already.
		return nil
	}
	return err
}

// Unwatch unwatches an address by removing it from the database.
// threadedAddressWatcher will then pick up on that change and apply it to skyd.
func (p *Promoter) Unwatch(ctx context.Context, addr types.UnlockHash) error {
	_, err := p.staticColWatchedAddresses().DeleteOne(ctx, p.newUnusedWatchedAddress(addr))
	return err
}

// TestAddressWatcher is a unit test for threadedAddressWatcher.
func TestAddressWatcher(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	inserted := make(map[types.UnlockHash]bool)
	deleted := make(map[types.UnlockHash]struct{})
	var mu sync.Mutex
	updateFn := func(unused bool, updates ...WatchedAddressUpdate) error {
		mu.Lock()
		defer mu.Unlock()
		for _, update := range updates {
			switch update.OperationType {
			case operationTypeInsert:
				inserted[update.Address] = unused
			case operationTypeDelete:
				deleted[update.Address] = struct{}{}
			case "update":
			default:
				t.Error("unexpected operation type", update.OperationType)
			}
		}
		return nil
	}

	p, node, err := newTestPromoterWithUpdateFunc(t.Name(), t.Name(), updateFn)
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
		for _, unused := range inserted {
			if !unused {
				t.Error("inserted address should be unused")
			}
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
	inserted2 := make(map[types.UnlockHash]bool)
	deleted2 := make(map[types.UnlockHash]bool)
	f2 := func(unused bool, updates ...WatchedAddressUpdate) error {
		mu.Lock()
		defer mu.Unlock()
		for _, update := range updates {
			switch update.OperationType {
			case operationTypeInsert:
				inserted2[update.Address] = unused
			case operationTypeDelete:
				deleted2[update.Address] = unused
			default:
				t.Error("unexpected operation type", update.OperationType)
			}
		}
		return nil
	}

	p2, node2, err := newTestPromoterWithUpdateFunc(t.Name()+"2", t.Name(), f2)
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
		// Make sure all inserted addresses are used. That's because a
		// single one of them was used which should cause the callback
		// getting called with unused=false.
		for _, unused := range inserted2 {
			if !unused {
				t.Error("inserted addresses should all be unused")
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
	t.Parallel()

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
		_, exists := addrsMap[addr.Address]
		if !exists {
			t.Fatal("addr doesn't exist")
		}
	}
}

// TestShouldGenerateAddresses is a unit test for staticShouldGenerateAddresses.
func TestShouldGenerateAddresses(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

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

	// Case exactly minUnusedAddresses. We alternate between inserting
	// addresses with no user field and addresses with a user field set to
	// the default value to make sure the methods counts both.
	for i := 0; i < int(minUnusedAddresses); i++ {
		var addr types.UnlockHash
		fastrand.Read(addr[:])
		if i%2 == 0 {
			_, err = p.staticColWatchedAddresses().InsertOne(context.Background(), p.newUnusedWatchedAddress(addr))
		} else {
			_, err = p.staticColWatchedAddresses().InsertOne(context.Background(), bson.M{
				"_id": addr.String(),
			})
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	shouldGenerate, err := p.staticShouldGenerateAddresses()
	if err != nil {
		t.Fatal(err)
	}
	if shouldGenerate {
		t.Fatal("should not generate new addresses")
	}

	// Delete any element.
	_, err = p.staticColWatchedAddresses().DeleteOne(context.Background(), bson.M{})
	if err != nil {
		t.Fatal(err)
	}
	shouldGenerate, err = p.staticShouldGenerateAddresses()
	if err != nil {
		t.Fatal(err)
	}
	if !shouldGenerate {
		t.Fatal("should generate new addresses")
	}
}

// TestAddressForUser is a unit test for AddressForUser and
// threadedRegenerateAddresses.
func TestAddressForUser(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

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

	// Double check there are no addresses.
	n, err := p.staticColWatchedAddresses().CountDocuments(p.staticBGCtx, bson.M{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("should be 0", n)
	}

	// There should be no addresses so staticShouldGenerateAddresses should
	// return 'true'.
	shouldGenerate, err := p.staticShouldGenerateAddresses()
	if err != nil {
		t.Fatal(err)
	}
	if !shouldGenerate {
		t.Fatal("should be true")
	}

	// Fetch an address for a user. This should fail because there are none
	// yet. But it will trigger the creation of new addresses.
	user := "user"
	_, err = p.AddressForUser(context.Background(), user)
	if !errors.Contains(err, mongo.ErrNoDocuments) {
		t.Fatal(err)
	}

	err = build.Retry(100, 100*time.Millisecond, func() error {
		// There should be maxUnusedAddresses now.
		n, err = p.staticColWatchedAddresses().CountDocuments(p.staticBGCtx, bson.M{})
		if err != nil {
			return err
		}
		if n != maxUnusedAddresses {
			return fmt.Errorf("wrong number of addresses %v != %v", n, maxUnusedAddresses)
		}
		// All of them should have the server set.
		n, err = p.staticColWatchedAddresses().CountDocuments(p.staticBGCtx, bson.M{
			"server": p.staticServerDomain,
		})
		if err != nil {
			return err
		}
		if n != maxUnusedAddresses {
			return fmt.Errorf("wrong number of addresses %v != %v", n, maxUnusedAddresses)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Try again.
	addr, err := p.AddressForUser(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if addr == (types.UnlockHash{}) {
		t.Fatal("invalid address")
	}

	// maxUnusedAddresses-1 should be unused.
	n, err = p.staticColWatchedAddresses().CountDocuments(p.staticBGCtx, filterUnusedAddresses)
	if err != nil {
		t.Fatal(err)
	}
	if n != maxUnusedAddresses-1 {
		t.Fatalf("should be %v was %v", maxUnusedAddresses-1, n)
	}

	// maxUnusedAddresses should still exist.
	n, err = p.staticColWatchedAddresses().CountDocuments(p.staticBGCtx, bson.M{})
	if err != nil {
		t.Fatal(err)
	}
	if n != maxUnusedAddresses {
		t.Fatalf("wrong number of addresses %v != %v", n, maxUnusedAddresses)
	}

	// Run it maxUnusedAddresses more times. It should always return the
	// same addr and the number of addresses should remain stable.
	for i := int64(0); i < maxUnusedAddresses; i++ {
		addrNew, err := p.AddressForUser(context.Background(), user)
		if err != nil {
			t.Fatal(err)
		}
		if addrNew != addr {
			t.Fatalf("address changed %v != %v", addr, addrNew)
		}
	}

	// maxUnusedAddresses-1 should be unused.
	n, err = p.staticColWatchedAddresses().CountDocuments(p.staticBGCtx, filterUnusedAddresses)
	if err != nil {
		t.Fatal(err)
	}
	if n != maxUnusedAddresses-1 {
		t.Fatalf("should be %v was %v", maxUnusedAddresses-1, n)
	}

	// maxUnusedAddresses should still exist.
	n, err = p.staticColWatchedAddresses().CountDocuments(p.staticBGCtx, bson.M{})
	if err != nil {
		t.Fatal(err)
	}
	if n != maxUnusedAddresses {
		t.Fatalf("wrong number of addresses %v != %v", n, maxUnusedAddresses)
	}
}

// TestInsertTransactions is a unit test for staticInsertTransactions.
func TestInsertTransactions(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

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

	// Get an address and send money to it multiple times to get multiple
	// addresses.
	wag, err := node.WalletAddressGet()
	if err != nil {
		t.Fatal(err)
	}
	addr := wag.Address
	nTxns := 10
	for i := 0; i < nTxns; i++ {
		_, err = node.WalletSiacoinsPost(types.SiacoinPrecision, addr, false)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := node.MineBlock(); err != nil {
		t.Fatal(err)
	}

	// Get the txns from skyd.
	txns, err := p.staticTxnsByAddress(addr)
	if err != nil {
		t.Fatal(err)
	}
	if len(txns) != nTxns {
		t.Fatalf("should have %v txn but was %v", nTxns, len(txns))
	}

	// Insert half of them.
	n, err := p.staticInsertTransactions(txns[:len(txns)/2])
	if err != nil {
		t.Fatal(err)
	}
	if n != len(txns)/2 {
		t.Fatalf("wrong number of txns inserted %v != %v", n, len(txns)/2)
	}

	// Insert all of them.
	n, err = p.staticInsertTransactions(txns)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(txns)/2 {
		t.Fatalf("wrong number of txns inserted %v != %v", n, len(txns)/2)
	}

	// The database should contain the txns.
	c, err := p.staticColTransactions().Find(context.Background(), bson.M{})
	if err != nil {
		t.Fatal(err)
	}
	var dbTxns []Transaction
	if err := c.All(context.Background(), &dbTxns); err != nil {
		t.Fatal(err)
	}
	if len(dbTxns) != len(txns) {
		t.Fatalf("wrong length %v != %v", len(dbTxns), len(txns))
	}

	// Txns should be the same.
	txnMap := make(map[types.TransactionID]Transaction)
	for _, txn := range txns {
		t := txn.(Transaction)
		txnMap[t.TxnID] = t
	}
	for _, txn := range dbTxns {
		foundTxn, found := txnMap[txn.TxnID]
		if !found {
			t.Fatal("not found")
		}
		if !reflect.DeepEqual(foundTxn, txn) {
			t.Log(foundTxn)
			t.Log(txn)
			t.Fatal("txn mismatch")
		}
	}
}
