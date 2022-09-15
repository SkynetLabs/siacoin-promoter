package test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SkynetLabs/siacoin-promoter/utils"
	"gitlab.com/SkynetLabs/skyd/build"
	"go.sia.tech/siad/types"
)

// TestSkydConnection creates a tester with a skyd client and makes sure that it
// shows up as healthy.
func TestSkydConnection(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	// Spin up skyd instance.
	node, err := utils.NewSkydForTesting(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Create a tester that can connect to the node.
	tester, err := newTester(&node.Client, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Query /health endpoint.
	hg, err := tester.Health()
	if err != nil {
		t.Fatal(err)
	}

	// Database and skyd should be alive.
	if !hg.DBAlive {
		t.Fatal("db is not alive")
	}
	if !hg.SkydAlive {
		t.Fatal("skyd isn't alive")
	}
}

// TestAddressEndpoint makes sure the address endpoint returns an address for a
// user and that subsequent calls return the same address.
func TestAddressEndpoint(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	// Spin up skyd instance.
	node, err := utils.NewSkydForTesting(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Create a tester that can connect to the node.
	tester, err := newTester(&node.Client, t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// The mocked accounts service will use the authorization and cookie
	// headers to derive a user sub. So we set them to foo and bar to get
	// the sub foo-bar.
	userSub := "foo-bar"
	headers := map[string]string{
		"Authorization": strings.Split(userSub, "-")[0],
		"Cookie":        strings.Split(userSub, "-")[1],
	}

	// Get address for a user.
	var addr types.UnlockHash
	err = build.Retry(100, 100*time.Millisecond, func() error {
		var err error
		addr, err = tester.PromoterClient.Address(headers)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if addr == (types.UnlockHash{}) {
		t.Fatal("invalid addr")
	}

	// Call it one more time.
	addr2, err := tester.PromoterClient.Address(headers)
	if err != nil {
		t.Fatal(err)
	}
	if addr != addr2 {
		t.Fatal("addresses don't match", addr, addr2)
	}

	// Create a promoter for checking the db directly for the expected sub.
	p, err := newTestPromoter(&node.Client, t.Name(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	addr3, err := p.AddressForUser(context.Background(), userSub)
	if err != nil {
		t.Fatal(err)
	}
	if addr != addr3 {
		t.Fatal("addresses don't match", addr, addr3)
	}
}
