package test

import (
	"testing"

	"github.com/SkynetLabs/siacoin-promoter/utils"
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
	tester, err := newTester(&node.Client)
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
