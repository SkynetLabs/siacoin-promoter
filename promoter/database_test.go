package promoter

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"gitlab.com/NebulousLabs/fastrand"
	"gitlab.com/SkynetLabs/skyd/build"
	"go.sia.tech/siad/crypto"
)

const (
	testUsername = "admin"
	// nolint:gosec // Disable gosec since these are only test credentials.
	testPassword = "aO4tV5tC1oU3oQ7u"
	testURI      = "mongodb://localhost:37017"
)

// newTestPromoter creates a Promoter instance for testing.
func newTestPromoter() (*Promoter, error) {
	// Create discard logger.
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return New(context.Background(), logrus.NewEntry(logger), testURI, testUsername, testPassword)
}

// newTestDBWithUpdateFunc creates a Promoter instance for testing.
func newTestDBWithUpdateFunc(f updateFunc) (*Promoter, error) {
	// Create discard logger.
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	ctx := context.Background()
	logEntry := logrus.NewEntry(logger)
	client, err := connect(ctx, logEntry, testURI, testUsername, testPassword)
	if err != nil {
		return nil, err
	}
	p := newPromoter(context.Background(), logEntry, client)
	p.initBackgroundThreads(f)
	return p, nil
}

// TestPing makes sure that we can connect to a database and ping it.
func TestPing(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	db, err := newTestPromoter()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
}

// TestAddressWatcher is a unit test for threadedAddressWatcher.
func TestAddressWatcher(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	inserted := make(map[crypto.Hash]struct{})
	deleted := make(map[crypto.Hash]struct{})
	var mu sync.Mutex
	f := func(update WatchedAddressesUpdate) {
		mu.Lock()
		defer mu.Unlock()
		switch update.OperationType {
		case "insert":
			inserted[update.DocumentKey.Address] = struct{}{}
		case "delete":
			deleted[update.DocumentKey.Address] = struct{}{}
		default:
			t.Error("unknown", update.OperationType)
		}
	}

	db, err := newTestDBWithUpdateFunc(f)
	if err != nil {
		t.Fatal(err)
	}

	// Add some addresses.
	var addrs []crypto.Hash
	for i := 0; i < 3; i++ {
		var addr crypto.Hash
		fastrand.Read(addr[:])
		addrs = append(addrs, addr)

		if err := db.Watch(context.Background(), addr); err != nil {
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
		if err := db.Unwatch(context.Background(), addr); err != nil {
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
}
