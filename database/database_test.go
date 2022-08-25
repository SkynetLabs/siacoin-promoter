package database

import (
	"context"
	"testing"
)

func newTestDB() (*Database, error) {
	username := "admin"
	password := "aO4tV5tC1oU3oQ7u"
	uri := "mongodb://localhost:37017"
	return New(context.Background(), uri, username, password)
}

func TestPing(t *testing.T) {
	db, err := newTestDB()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
}
