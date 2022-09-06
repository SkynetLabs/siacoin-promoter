package promoter

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
	"gitlab.com/NebulousLabs/errors"
	"go.sia.tech/siad/types"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
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

	updateMaxBatchSize = 10000
)

type (
	// operationType describes the operation type of an update to the
	// watched addresses collection.
	operationType string

	// updateFunc is the type of a function that can be used as a callback
	// in threadedAddressWatcher. Unused determines whether or not the
	// 'unsed' flag is set in the API request for new addresses to watch.
	// For deletions it will always be set to 'false'.
	updateFunc func(unused bool, updates ...WatchedAddressUpdate) error

	User struct {
		ID  primitive.ObjectID `bson:"_id"`
		Sub string             `bson:"sub"`
	}

	// WatchedAddressInsert describes an entry in the watched address
	// collection.
	WatchedAddress struct {
		// Address is the actual address we track. We make that the _id
		// of the object since the addresses should be indexed and
		// unique anyway.
		Address types.UnlockHash `bson:"_id"`

		// User is the user that the address is assigned to. 0 if the
		// address is unused.
		User primitive.ObjectID `bson:"user"`
	}

	// WatchedAddressDBUpdate describes an update to the watched address
	// collection in the db.
	WatchedAddressDBUpdate struct {
		DocumentKey struct {
			Address types.UnlockHash `bson:"_id"`
		} `bson:"documentKey"`
		FullDocument  WatchedAddress `bson:"fullDocument"`
		OperationType operationType  `bson:"operationType"`
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

// Unused returns whether the watched address is currently not assigned to a
// user.
func (w *WatchedAddress) Unused() bool {
	return w.User.IsZero()
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
func (p *Promoter) staticWatchedDBAddresses(ctx context.Context) ([]WatchedAddress, error) {
	c, err := p.staticColWatchedAddresses().Find(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	var addrs []WatchedAddress
	for c.Next(ctx) {
		var addr WatchedAddress
		if err := c.Decode(&addr); err != nil {
			return nil, err
		}
		addrs = append(addrs, addr)
	}
	return addrs, nil
}

// threadedAddressWatcher listens syncs skyd's and the database's watched
// addresses and then continues listening for changes to the watched addresses.
func (p *Promoter) threadedAddressWatcher(ctx context.Context, updateFn updateFunc) {
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
		toRemoveUpdates := make([]WatchedAddressUpdate, 0, len(toRemove))
		toAddUpdates := make([]WatchedAddressUpdate, 0, len(toAdd))
		unused := true
		for _, addr := range toAdd {
			unused = unused && addr.Unused()
			toAddUpdates = append(toAddUpdates, WatchedAddressUpdate{
				Address:       addr.Address,
				OperationType: operationTypeInsert,
			})
		}
		for _, addr := range toRemove {
			toRemoveUpdates = append(toRemoveUpdates, WatchedAddressUpdate{
				Address:       addr,
				OperationType: operationTypeDelete,
			})
		}

		// To avoid resyncing skyd's wallet too often, we check all
		// possible edge cases and make sure we only call updateFn with
		// unused = false once.
		if len(toAddUpdates) > 0 && len(toRemoveUpdates) == 0 {
			err = updateFn(unused, toAddUpdates...)
		} else if len(toAddUpdates) == 0 && len(toRemoveUpdates) > 0 {
			err = updateFn(false, toRemoveUpdates...)
		} else if len(toAddUpdates) > 0 && len(toRemoveUpdates) > 0 {
			err1 := updateFn(true, toRemoveUpdates...)
			err2 := updateFn(false, toAddUpdates...)
			err = errors.Compose(err1, err2)
		}
		if err != nil {
			p.staticLogger.WithError(err).Error("Failed to update skyd with initial diff")
			time.Sleep(2 * time.Second) // sleep before retrying
			continue OUTER              // try again
		}

		// Start listening for future changes. We block for a change
		// first and then we check for more changes in a non-blocking
		// fashion up until a certain batch size. That way we reduce the
		// number of requests to skyd.
		for stream.Next(ctx) {
			var updates []WatchedAddressUpdate
			unused = true
			for {
				// Decode the entry.
				var wa WatchedAddressDBUpdate
				if err := stream.Decode(&wa); err != nil {
					p.staticLogger.WithError(err).Error("Failed to decode watched address")
					time.Sleep(2 * time.Second) // sleep before retrying
					continue OUTER              // try again
				}
				unused = unused && wa.FullDocument.Unused()
				updates = append(updates, wa.ToUpdate())

				// Check if there is more. If not, we continue
				// the blocking loop.
				if len(updates) == updateMaxBatchSize || !stream.TryNext(ctx) {
					break
				}
			}
			// Apply the updates.
			if err := updateFn(unused, updates...); err != nil {
				p.staticLogger.WithError(err).Error("Failed to update skyd with incoming change")
				time.Sleep(2 * time.Second) // sleep before retrying
				continue OUTER              // try again
			}
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
