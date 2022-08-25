package database

import (
	"context"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readconcern"
	"go.mongodb.org/mongo-driver/mongo/readpref"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"
)

type (
	// Database is a wrapper for the connection to the database and
	// abstracts all interactions with the database.
	Database struct {
		staticClient *mongo.Client

		ctx context.Context
	}
)

// New creates a new database from the given credentials.
func New(ctx context.Context, uri, username, password string) (*Database, error) {
	// Connect to database.
	creds := options.Credential{
		Username: username,
		Password: password,
	}
	opts := options.Client().
		ApplyURI(uri).
		SetAuth(creds).
		SetReadConcern(readconcern.Local()).
		SetReadPreference(readpref.Nearest()).
		SetWriteConcern(writeconcern.New(writeconcern.WMajority()))

	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Create store.
	db := &Database{
		ctx:          ctx,
		staticClient: client,
	}
	return db, nil
}

// Close closes the connection to the database.
func (db *Database) Close() error {
	return db.staticClient.Disconnect(db.ctx)
}

// Ping uses the lowest readpref to determine whether the database connection is
// healthy at the moment.
func (db *Database) Ping() error {
	return db.staticClient.Ping(db.ctx, readpref.Nearest())
}
