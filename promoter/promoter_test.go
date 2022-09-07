package promoter

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/SkynetLabs/siacoin-promoter/utils"
	"github.com/sirupsen/logrus"
	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/fastrand"
	"gitlab.com/SkynetLabs/skyd/siatest"
	"go.sia.tech/siad/types"
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
	p, err := New(context.Background(), &skyd.Client, logrus.NewEntry(logger), testURI, testUsername, testPassword, name)
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logEntry := logrus.NewEntry(logger)
	client, err := connect(ctx, logEntry, testURI, testUsername, testPassword)
	if err != nil {
		return nil, nil, err
	}
	p, err := newPromoter(context.Background(), &skyd.Client, logEntry, client, name)
	if err != nil {
		return nil, nil, errors.Compose(err, client.Disconnect(ctx))
	}
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

// TestAddrDiff is a unit test for staticAddrDiff.
func TestAddrDiff(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	p, node, err := newTestPromoterWithUpdateFunc(t.Name(), func(_ bool, _ ...WatchedAddressUpdate) error {
		// Don't do anything.
		return nil
	})
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

	// Create some addresses.
	var addr1 types.UnlockHash
	fastrand.Read(addr1[:])
	var addr2 types.UnlockHash
	fastrand.Read(addr2[:])
	var addr3 types.UnlockHash
	fastrand.Read(addr3[:])

	// Add the first two to the db.
	if err := p.Watch(context.Background(), addr1); err != nil {
		t.Fatal(err)
	}
	if err := p.Watch(context.Background(), addr2); err != nil {
		t.Fatal(err)
	}

	// Add the latter two to skyd.
	err = p.staticSkyd.WalletWatchAddPost([]types.UnlockHash{addr2, addr3}, true)
	if err != nil {
		t.Fatal(err)
	}

	// The diff should now result in 1 address for adding and 1 for removal.
	toAdd, toRemove, err := p.staticAddrDiff(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(toAdd) != 1 {
		t.Fatal("should have one address to add", toAdd)
	}
	if len(toRemove) != 1 {
		t.Fatal("should have one address to remove", toRemove)
	}
	if toAdd[0].Address != addr1 {
		t.Fatal("addr1 should be the one to add")
	}
	if toAdd[0].Unused() != true {
		t.Fatal("addr1 should be unused")
	}
	if toRemove[0] != addr3 {
		t.Fatal("addr3 should be the one to remove")
	}
}
