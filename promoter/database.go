package promoter

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
	lock "github.com/square/mongo-lock"
	"gitlab.com/NebulousLabs/errors"
	"go.sia.tech/siad/build"
	"go.sia.tech/siad/types"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readconcern"
	"go.mongodb.org/mongo-driver/mongo/readpref"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"
)

const (
	colLocksName            = "locks"
	colWatchedAddressesName = "watched_addresses"

	lockTTL             = 300 // seconds
	lockPruningInterval = 24 * time.Hour

	operationTypeInsert = operationType("insert")
	operationTypeDelete = operationType("delete")
)

// filterUnusedAddresses is the filter used by queries interested in the number
// of addressed not assigned to any user.
var filterUnusedAddresses = bson.M{
	"$or": bson.A{
		bson.M{"user_id": bson.M{"$exists": false}}, // field is not set
		bson.M{"user_id": ""},                       // field is set to default value
	},
}

var (
	// minUnusedAddresses is the min number of addresses we want to keep in
	// the db which are not yet assigned to users. If the number drops below
	// this, we generate more addresses.
	minUnusedAddresses = build.Select(build.Var{
		Testing:  int64(5),
		Dev:      int64(50),
		Standard: int64(5000),
	}).(int64)

	// maxUnusedAddresses is the max number of addresses we want to keep in
	// the db which are not yet assigned to users.
	maxUnusedAddresses = build.Select(build.Var{
		Testing:  int64(10),
		Dev:      int64(100),
		Standard: int64(10000),
	}).(int64)

	// updateMaxBatchSize is the max number of addresses we send to skyd
	// within a single API request.
	updateMaxBatchSize = minUnusedAddresses
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

	// User is the type of a user in the database.
	// TODO: f/u with API endpoint for creating users and assigning
	// addresses.
	User struct {
		Sub string `bson:"_id"`

		// TODO: f/u with a PR to poll for transactions. Store them in a
		// transactions array together with the amount of incoming funds
		// after the promoter service was notified of the new txn.
	}

	// WatchedAddress describes an entry in the watched address collection.
	WatchedAddress struct {
		// Address is the actual address we track. We make that the _id
		// of the object since the addresses should be indexed and
		// unique anyway.
		Address types.UnlockHash `bson:"_id"`

		// Server defines the server that created this address. This is
		// useful for tracking which addresses belong to which server
		// and as a result to which seed.
		Server string `bson:"server"`

		// UserSub is the user that the address is assigned to. 0 if the
		// address is unused.
		UserSub string `bson:"user_id"`
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
	return w.UserSub == ""
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

// AddressForUser returns an address for a user. If there is no such address,
// fetch one from the pool. Then check if the pool needs to be topped up.
// TODO: Once we add support for fetching users new addresses we need to make
// sure we add an 'active' flag to indicate which of the associated addresses to
// fetch.
func (p *Promoter) AddressForUser(ctx context.Context, sub string) (types.UnlockHash, error) {
	// Fetch address of user.
	sr := p.staticColWatchedAddresses().FindOne(ctx, bson.M{
		"user_id": sub,
	})
	var wa WatchedAddress
	err := sr.Decode(&wa)
	if err == nil {
		return wa.Address, nil // return existing address
	}
	if err != nil && !errors.Contains(err, mongo.ErrNoDocuments) {
		p.staticLogger.WithError(err).Error("Failed to look for existing user address")
		return types.UnlockHash{}, err
	}

	// If there was no address, fetch one from the pool.
	sr = p.staticColWatchedAddresses().FindOneAndUpdate(ctx, filterUnusedAddresses, bson.M{
		"$set": bson.M{
			"user_id": sub,
		},
	})
	err = sr.Decode(&wa)
	if err != nil && !errors.Contains(err, mongo.ErrNoDocuments) {
		p.staticLogger.WithError(err).Error("Failed to acquire new address for user")
		return types.UnlockHash{}, err
	}

	// Kick off goroutine to check if regenerating the pool is necessary in
	// both the successful case as well as the ErrNoDocuments case. The
	// latter should never happen but we still try to handle it by
	// generating new addresses.
	p.staticWG.Add(1)
	go func() {
		p.threadedRegenerateAddresses()
		p.staticWG.Done()
	}()

	return wa.Address, err
}

// Close closes the connection to the database.
func (p *Promoter) Close() error {
	// Cancel background threads.
	p.staticThreadCancel()

	// Wait for them to finish.
	p.staticWG.Wait()

	// Disconnect from db.
	return p.staticDB.Client().Disconnect(p.staticCtx)
}

// newUnusedWatchedAddress creates a new WatchedAddress for this promoter that
// doesnt' have a User assigned yet.
func (p *Promoter) newUnusedWatchedAddress(addr types.UnlockHash) WatchedAddress {
	return WatchedAddress{
		Address: addr,
		Server:  p.staticServerDomain,
	}
}

// staticColLocks returns the collection used to store locks.
func (p *Promoter) staticColLocks() *mongo.Collection {
	return p.staticDB.Collection(colLocksName)
}

// staticColWatchedAddresses returns the collection used to store watched
// addresses.
func (p *Promoter) staticColWatchedAddresses() *mongo.Collection {
	return p.staticDB.Collection(colWatchedAddressesName)
}

// staticShouldGenerateAddresses returns whether or not we should try to
// generate new addresses. This method doesn't use any locking to provide a
// quick way to estimate whether we might need to generate new addresses. The
// actual address generating code should lock the collection, fetch the actual
// number of addresses and add new ones accordingly.
func (p *Promoter) staticShouldGenerateAddresses() (bool, error) {
	n, err := p.staticColWatchedAddresses().CountDocuments(p.staticBGCtx, filterUnusedAddresses, options.Count().SetLimit(minUnusedAddresses))
	if err != nil {
		return false, err
	}
	return n < minUnusedAddresses, nil
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

		// Track whether any of the addresses to be added is considered
		// used. If any of them are, we need to call updateFn with
		// unused = false to make sure we trigger a blockchain rescan in
		// skyd to pick up on potential transactions from the past.
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
			unused = true // track if any addresses are used as before.
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
				if int64(len(updates)) == updateMaxBatchSize || !stream.TryNext(ctx) {
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

// threadedPruneLocks periodically scans the db for prunable locks.
func (p *Promoter) threadedPruneLocks() {
	t := time.NewTicker(lockPruningInterval)
	defer t.Stop()

	purger := lock.NewPurger(p.staticLockClient)
	for range t.C {
		select {
		case <-p.staticBGCtx.Done():
			return
		default:
		}

		_, err := purger.Purge(p.staticBGCtx)
		if err != nil {
			p.staticLogger.WithTime(time.Now()).WithError(err).Error("Purging locks failed")
		}
	}
}

// threadedRegenerateAddresses checks whether new addresses need to be generated
// and then generates enough addresses to restore the pool of unused addresses
// to maxUnusedAddresses.
func (p *Promoter) threadedRegenerateAddresses() {
	// Do a fast check first. This is not accurate but might help us to
	// avoid a write to the db in most cases.
	shouldGenerate, err := p.staticShouldGenerateAddresses()
	if err != nil {
		p.staticLogger.WithError(err).Error("Failed to check whether regenerating the address pool is necessary")
		return
	}
	if !shouldGenerate {
		return // nothing to do
	}

	// Lock the collection.
	err = p.staticLockClient.XLock(p.staticBGCtx, "watched-addresses", "watched-addresses", lock.LockDetails{
		Owner: "siacoin-promoter",
		Host:  p.staticServerDomain,
		TTL:   lockTTL,
	})
	if err == lock.ErrAlreadyLocked {
		p.staticLogger.Debug("Not generating new addresses because the collection is already locked")
		return // nothing to do
	}

	// Unlock when we are done.
	defer func() {
		if _, err := p.staticLockClient.Unlock(p.staticBGCtx, "watched-addresses"); err != nil {
			p.staticLogger.WithError(err).Error("Failed to unlock lock over watched addresses collection")
		}
	}()

	// TODO: check number of unused addresses.
	n, err := p.staticColWatchedAddresses().CountDocuments(p.staticBGCtx, filterUnusedAddresses)
	if err != nil {
		p.staticLogger.WithError(err).Error("Failed to fetch count of unused addresses for generating new ones")
		return
	}

	// Figure out how many to generate.
	toGenerate := maxUnusedAddresses - n
	if toGenerate <= 0 {
		p.staticLogger.WithField("toGenerate", toGenerate).Debug("Not generating new addresses because the collection has enough")
		return // nothing to do
	}

	p.staticLogger.WithField("toGenerate", toGenerate).Info("Starting to generate new addresses")

	// Generate the new addresses. We have to do this one-by-one since skyd
	// doesn't have an endpoint for address batch creation.
	newAddresses := make([]interface{}, 0, toGenerate)
	for i := int64(0); i < toGenerate; i++ {
		wag, err := p.staticSkyd.WalletAddressGet()
		if err != nil {
			p.staticLogger.WithError(err).Error("Failed to fetch new address from skyd")
			return
		}
		newAddresses = append(newAddresses, p.newUnusedWatchedAddress(wag.Address))
	}

	// Insert them into the db.
	_, err = p.staticColWatchedAddresses().InsertMany(p.staticBGCtx, newAddresses)
	if err != nil {
		p.staticLogger.WithError(err).Error("Failed to store generated address in db.")
		return
	}
}
