package utils

import (
	"os"
	"path/filepath"

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

// NewSkydForTesting launches a full skyd node with a wallet for testing.
func NewSkydForTesting(testName string) (*siatest.TestNode, error) {
	// Create a new wallet.
	walletParams := node.Wallet(testDir(testName))
	walletParams.CreateMiner = true // also need a miner
	return siatest.NewNode(walletParams)
}
