package database

import (
	"context"
	"testing"
)

// newTestDB creates a Database instance for testing.
func newTestDB() (*Database, error) {
	username := "admin"
	// nolint:gosec // Disable gosec since these are only test credentials.
	password := "aO4tV5tC1oU3oQ7u"
	uri := "mongodb://localhost:37017"
	return New(context.Background(), uri, username, password)
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
