package database

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

// newTestDB creates a Database instance for testing.
func newTestDB() (*Database, error) {
	// Create discard logger.
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return New(context.Background(), logrus.NewEntry(logger), testURI, testUsername, testPassword)
}

// newTestDBWithUpdateFunc creates a Database instance for testing.
func newTestDBWithUpdateFunc(f updateFunc) (*Database, error) {
	// Create discard logger.
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	db, err := connect(context.Background(), logrus.NewEntry(logger), testURI, testUsername, testPassword)
	if err != nil {
		return nil, err
	}
	db.initBackgroundThreads(f)
	return db, nil
}

// TestPing makes sure that we can connect to a database and ping it.
func TestPing(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	db, err := newTestDB()
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
			fmt.Println("inserted", update.DocumentKey.Address)
			inserted[update.DocumentKey.Address] = struct{}{}
		case "delete":
			fmt.Println("removed", update.DocumentKey.Address)
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

	// Remove them again.
	for _, addr := range addrs {
		if err := db.Unwatch(context.Background(), addr); err != nil {
			t.Fatal(err)
		}
	}

	// Run check in loop since it's async.
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
