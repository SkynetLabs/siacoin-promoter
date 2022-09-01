package test

import (
	"os"
	"path/filepath"
	"testing"

	"gitlab.com/SkynetLabs/skyd/node"
	"gitlab.com/SkynetLabs/skyd/siatest"
)

// promoterTestingDir defines the root dir of the promoter tests.
var promoterTestingDir = filepath.Join(os.TempDir(), "SiacoinPromoter")

// TestDir joins the provided directories and prefixes them with the Sia
// testing directory, removing any files or directories that previously existed
// at that location.
func testDir(testName string) string {
	path := filepath.Join(promoterTestingDir, testName)
	err := os.RemoveAll(path)
	if err != nil {
		panic(err)
	}
	if err := os.MkdirAll(path, 0750); err != nil {
		panic(err)
	}
	return path
}

// newSkydForTesting launches a full skyd node with a wallet for testing.
func newSkydForTesting(testName string) (*siatest.TestNode, error) {
	// Create a new wallet.
	walletParams := node.Wallet(testDir(testName))
	walletParams.CreateMiner = true // also need a miner
	return siatest.NewNode(walletParams)
}

// TestSkydConnection creates a tester with a skyd client and makes sure that it
// shows up as healthy.
func TestSkydConnection(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	// Spin up skyd instance.
	node, err := newSkydForTesting(t.Name())
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
