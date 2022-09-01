package test

import (
	"testing"
)

// TestHealth is a simple smoke test to verify the basic functionality of the
// tester by querying the API's /health endpoint.
func TestHealth(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	// Create tester.
	tester, err := newTester(nil) // no skyd connection needed
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
	if hg.SkydAlive {
		t.Fatal("skyd shouldn't be alive since we didn't pass in a client")
	}
}
