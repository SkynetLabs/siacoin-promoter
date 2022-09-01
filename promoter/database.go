package promoter

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
	"go.sia.tech/siad/crypto"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readconcern"
	"go.mongodb.org/mongo-driver/mongo/readpref"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"
)

const (
	dbName                  = "siacoinpromoter"
	colWatchedAddressesName = "watchedaddresses"
)

type (
	// updateFunc is the type of a function that can be used as a callback
	// in threadedAddressWatcher.
	updateFunc func(WatchedAddressesUpdate)

	// WatchedAddress describes an entry in the watched address collection.
	WatchedAddress struct {
		// Address is the actual address we track. We make that the _id
		// of the object since the addresses should be indexed and
		// unique anyway.
		Address crypto.Hash `bson:"_id"`
	}

	// WatchedAddressesUpdate describes an update to the watched address
	// collection.
	WatchedAddressesUpdate struct {
		DocumentKey struct {
			Address crypto.Hash `bson:"_id"`
		} `bson:"documentKey"`
		OperationType string `bson:"operationType"`
	}
)

// connect creates a new database object that is connected to a mongodb.
func connect(ctx context.Context, log *logrus.Entry, uri, username, password string) (*mongo.Client, error) {
	// Connect to database.
	creds := options.Credential{
		Username: username,
		Password: password,
	}
	opts := options.Client().
		ApplyURI(uri).
		SetAuth(creds).
		SetReadConcern(readconcern.Majority()).
		SetReadPreference(readpref.Nearest()).
		SetWriteConcern(writeconcern.New(writeconcern.WMajority()))

	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// Watch watches an address by adding it to the database.
// threadedAddressWatcher will then pick up on that change and apply it to skyd.
func (p *Promoter) Watch(ctx context.Context, addr crypto.Hash) error {
	_, err := p.staticColWatchedAddresses.InsertOne(ctx, WatchedAddress{
		Address: addr,
	})
	return err
}

// Unwatch unwatches an address by removing it from the database.
// threadedAddressWatcher will then pick up on that change and apply it to skyd.
func (p *Promoter) Unwatch(ctx context.Context, addr crypto.Hash) error {
	_, err := p.staticColWatchedAddresses.DeleteOne(ctx, WatchedAddress{
		Address: addr,
	})
	return err
}

// threadedAddressWatcher listens syncs skyd's and the database's watched
// addresses and then continues listening for changes to the watched addresses.
func (p *Promoter) threadedAddressWatcher(ctx context.Context, f updateFunc) {
	// NOTE: The outter loop is a fallback mechanism in case of an error.
	// During successful operations it should only do one full iteration.
OUTER:
	for {
		select {
		case <-ctx.Done():
			return // shutdown
		default:
		}

		// Start watching the collection.
		stream, err := p.staticColWatchedAddresses.Watch(ctx, mongo.Pipeline{})
		if err != nil {
			p.staticLogger.WithError(err).Error("Failed to start watching address collection")
			time.Sleep(2 * time.Second) // sleep before retrying
			continue OUTER              // try again
		}

		// TODO: Fetch all watched addresses and compare them to skyd's
		// watched addresses. We need to sync the database and skyd up
		// before we can start relying on the change stream.

		// Start listening for changes.
		for stream.Next(ctx) {
			var wa WatchedAddressesUpdate
			if err := stream.Decode(&wa); err != nil {
				p.staticLogger.WithError(err).Error("Failed to decode watched address")
				time.Sleep(2 * time.Second) // sleep before retrying
				continue OUTER              // try again
			}
			f(wa)
		}
	}
}

// Close closes the connection to the database.
func (p *Promoter) Close() error {
	// Cancel background threads.
	p.threadCancel()

	// Wait for them to finish.
	p.wg.Wait()

	// Disconnect from db.
	return p.staticClient.Disconnect(p.ctx)
}

// Ping uses the lowest readpref to determine whether the database connection is
// healthy at the moment.
func (p *Promoter) Ping() error {
	return p.staticClient.Ping(p.ctx, nil)
}