package promoter

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
	"go.sia.tech/siad/types"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readconcern"
	"go.mongodb.org/mongo-driver/mongo/readpref"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"
)

const (
	dbName                  = "siacoin-promoter"
	colWatchedAddressesName = "watched_addresses"

	operationTypeInsert = operationType("insert")
	operationTypeDelete = operationType("delete")
)

type (
	// operationType describes the operation type of an update to the
	// watched addresses collection.
	operationType string

	// updateFunc is the type of a function that can be used as a callback
	// in threadedAddressWatcher.
	updateFunc func(WatchedAddressUpdate)

	// WatchedAddress describes an entry in the watched address collection.
	WatchedAddress struct {
		// Address is the actual address we track. We make that the _id
		// of the object since the addresses should be indexed and
		// unique anyway.
		Address types.UnlockHash `bson:"_id"`
	}

	// WatchedAddressDBUpdate describes an update to the watched address
	// collection in the db.
	WatchedAddressDBUpdate struct {
		DocumentKey struct {
			Address types.UnlockHash `bson:"_id"`
		} `bson:"documentKey"`
		OperationType operationType `bson:"operationType"`
	}

	// WatchedAddressUpdate describes an update to the watched address
	// collection in memory.
	WatchedAddressUpdate struct {
		Address       types.UnlockHash
		OperationType operationType
	}
)

// ToUpdate turns the WatchedAddressDBUpdate into a WatchedAddressUpdate.
func (u *WatchedAddressDBUpdate) ToUpdate() WatchedAddressUpdate {
	return WatchedAddressUpdate{
		Address:       u.DocumentKey.Address,
		OperationType: u.OperationType,
	}
}

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
func (p *Promoter) Watch(ctx context.Context, addr types.UnlockHash) error {
	_, err := p.staticColWatchedAddresses().InsertOne(ctx, WatchedAddress{
		Address: addr,
	})
	if mongo.IsDuplicateKeyError(err) {
		// nothing to do, the ChangeStream should've picked up on that
		// already.
		return nil
	}
	return err
}

// Unwatch unwatches an address by removing it from the database.
// threadedAddressWatcher will then pick up on that change and apply it to skyd.
func (p *Promoter) Unwatch(ctx context.Context, addr types.UnlockHash) error {
	_, err := p.staticColWatchedAddresses().DeleteOne(ctx, WatchedAddress{
		Address: addr,
	})
	return err
}

// staticColWatchedAddresses returns the collection used to store watched
// addresses.
func (p *Promoter) staticColWatchedAddresses() *mongo.Collection {
	return p.staticDB.Collection(colWatchedAddressesName)
}

// staticWatchedDBAddresses returns all watched addresses as seen in the
// database.
func (p *Promoter) staticWatchedDBAddresses(ctx context.Context) ([]types.UnlockHash, error) {
	c, err := p.staticColWatchedAddresses().Find(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	var addrs []types.UnlockHash
	for c.Next(ctx) {
		var addr WatchedAddress
		if err := c.Decode(&addr); err != nil {
			return nil, err
		}
		addrs = append(addrs, addr.Address)
	}
	return addrs, nil
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
		stream, err := p.staticColWatchedAddresses().Watch(ctx, mongo.Pipeline{})
		if err != nil {
			p.staticLogger.WithError(err).Error("Failed to start watching address collection")
			time.Sleep(2 * time.Second) // sleep before retrying
			continue OUTER              // try again
		}

		// Fetch the diff of watched addresses and send updates down the
		// callback accordingly.
		toAdd, toRemove, err := p.staticAddrDiff(ctx)
		if err != nil {
			p.staticLogger.WithError(err).Error("Failed to fetch address diff")
			time.Sleep(2 * time.Second) // sleep before retrying
			continue OUTER              // try again
		}
		for _, addr := range toAdd {
			f(WatchedAddressUpdate{
				Address:       addr,
				OperationType: operationTypeInsert,
			})
		}
		for _, addr := range toRemove {
			f(WatchedAddressUpdate{
				Address:       addr,
				OperationType: operationTypeDelete,
			})
		}

		// Start listening for future changes.
		for stream.Next(ctx) {
			var wa WatchedAddressDBUpdate
			if err := stream.Decode(&wa); err != nil {
				p.staticLogger.WithError(err).Error("Failed to decode watched address")
				time.Sleep(2 * time.Second) // sleep before retrying
				continue OUTER              // try again
			}
			f(wa.ToUpdate())
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
	return p.staticDB.Client().Disconnect(p.ctx)
}
