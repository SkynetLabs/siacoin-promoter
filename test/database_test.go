package test

import (
	"testing"
	"time"

	"github.com/SkynetLabs/siacoin-promoter/utils"
	"gitlab.com/SkynetLabs/skyd/build"
	"go.sia.tech/siad/types"
)

// TestHealth is a simple smoke test to verify the basic functionality of the
// tester by querying the API's /health endpoint.
func TestHealth(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Spin up skyd instance.
	node, err := utils.NewSkydForTesting(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := node.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Create tester.
	tester, err := newTester(&node.Client, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tester.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Query /health endpoint.
	hg, err := tester.Health()
	if err != nil {
		t.Fatal(err)
	}

	// Database should be alive but not skyd.
	if !hg.DBAlive {
		t.Fatal("db should be alive")
	}
	if !hg.SkydAlive {
		t.Fatal("skyd should be alive")
	}
}

// TestDeadServer is a test for the /dead/:servername endpoint.
func TestDeadServer(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Spin up skyd instance.
	node, err := utils.NewSkydForTesting(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := node.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Create tester.
	tester, err := newTester(&node.Client, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tester.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Get an address for a user. Might take a bit to generate the pool.
	headers := map[string]string{
		"Authorization": "foo",
		"Cookie":        "bar",
	}
	var addr types.UnlockHash
	err = build.Retry(100, 100*time.Millisecond, func() error {
		addr, err = tester.Address(headers)
		return err
	})

	// Mark the server dead.
	err = tester.MarkServerDead(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Fetch another address. Shouldn't be the same since the old one
	// belonged to this server and was marked as !primary.
	addrNew, err := tester.Address(headers)
	if err != nil {
		t.Fatal(err)
	}
	if addr == addrNew {
		t.Fatal("addresses shouldn't match")
	}
}
