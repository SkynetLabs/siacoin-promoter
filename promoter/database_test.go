package promoter

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/SkynetLabs/siacoin-promoter/utils"
	"github.com/sirupsen/logrus"
	"gitlab.com/NebulousLabs/fastrand"
	"gitlab.com/SkynetLabs/skyd/build"
	"gitlab.com/SkynetLabs/skyd/siatest"
	"go.sia.tech/siad/types"
)

const (
	testUsername = "admin"
	// nolint:gosec // Disable gosec since these are only test credentials.
	testPassword = "aO4tV5tC1oU3oQ7u"
	testURI      = "mongodb://localhost:37017"
)

// newTestPromoter creates a Promoter instance for testing.
func newTestPromoter(name string) (*Promoter, *siatest.TestNode, error) {
	// Create discard logger.
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	skyd, err := utils.NewSkydForTesting(name)
	if err != nil {
		return nil, nil, err
	}
	p, err := New(context.Background(), &skyd.Client, logrus.NewEntry(logger), testURI, testUsername, testPassword)
	if err != nil {
		return nil, nil, err
	}
	return p, skyd, nil
}

// newTestPromoterWithUpdateFunc creates a Promoter instance for testing.
func newTestPromoterWithUpdateFunc(name string, f updateFunc) (*Promoter, *siatest.TestNode, error) {
	// Create discard logger.
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	skyd, err := utils.NewSkydForTesting(name)
	if err != nil {
		return nil, nil, err
	}
	ctx := context.Background()
	logEntry := logrus.NewEntry(logger)
	client, err := connect(ctx, logEntry, testURI, testUsername, testPassword)
	if err != nil {
		return nil, nil, err
	}
	p := newPromoter(context.Background(), &skyd.Client, logEntry, client)
	p.initBackgroundThreads(f)
	return p, skyd, nil
}

// TestPromoterHealth is a unit test for the promoter's Health method.
func TestPromoterHealth(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	p, _, err := newTestPromoter(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	if ph := p.Health(); ph.Database != nil || ph.Skyd != nil {
		t.Fatal("not healthy", ph)
	}
}

// TestAddressWatcher is a unit test for threadedAddressWatcher.
func TestAddressWatcher(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	inserted := make(map[types.UnlockHash]struct{})
	deleted := make(map[types.UnlockHash]struct{})
	var mu sync.Mutex
	f := func(update WatchedAddressesUpdate) {
		mu.Lock()
		defer mu.Unlock()
		switch update.OperationType {
		case "insert":
			inserted[update.DocumentKey.Address] = struct{}{}
		case "delete":
			deleted[update.DocumentKey.Address] = struct{}{}
		case "drop":
		case "invalidate":
		default:
			t.Error("unknown", update.OperationType)
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
}
