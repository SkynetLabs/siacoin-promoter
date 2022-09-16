package promoter

import (
	"context"
	"math/big"
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
	colConfigName           = "config"
	colLocksName            = "locks"
	colWatchedAddressesName = "watched_addresses"
	colTransactionsName     = "transactions"

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
	// defaultConversionRate from SC to Credits is 1 SC -> 1 Credit.
	defaultConversionRate = new(big.Rat).SetFrac(big.NewInt(1), types.SiacoinPrecision.Big())

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

	// configIDConversionRate is the ID of the currency conversion rate in
	// the config collection.
	configIDConversionRate = "conversion_rate"
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

	// ConfigConversionRate is the representation of the conversion rate
	// within the db. To preserve precision up until the point of actually
	// converting siacoins to credits, we use a numerator/denominator pair.
	ConfigConversionRate struct {
		Numerator   string `bson:"numerator"`
		Denominator string `bson:"denominator"`
	}

	// User is the type of a user in the database.
	User struct {
		Sub string `bson:"_id"`
	}

	// Transaction describes a single transaction within the transaction
	// collection. They serve as receipts for incoming payments for users
	// and as a reference for which transactions we credited the user for
	// already by contacting the credit promoter.
	Transaction struct {
		Address    types.UnlockHash    `bson:"address_id"`
		Credited   bool                `bson:"credited"`
		CreditedAt time.Time           `bson:"credited_at"`
		TxnID      types.TransactionID `bson:"_id"`

		// Value is a stringified types.Currency since types.Currency is too large for
		// other types and Mongo can't seem to deal with it.
		Value string `bson:"value"`
	}

	// WatchedAddress describes an entry in the watched address collection.
	WatchedAddress struct {
		// Primary indicates whether the address is the user's primary
		// address. If no primary address can be found, a new address
		// will be fetched from the pool and made primary.
		Primary bool `bson:"primary"`

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

// Rat returns the conversion rate as a big.Rat.
func (cr ConfigConversionRate) Rat() (*big.Rat, bool) {
	num, ok := new(big.Int).SetString(cr.Numerator, 10)
	if !ok {
		build.Critical("failed to parse numerator")
		return nil, false
	}
	denom, ok := new(big.Int).SetString(cr.Denominator, 10)
	if !ok {
		build.Critical("failed to parse denominator")
		return nil, false
	}
	return new(big.Rat).SetFrac(num, denom), true
}

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
func (p *Promoter) AddressForUser(ctx context.Context, sub string) (types.UnlockHash, error) {
	// Fetch address of user.
	sr := p.staticColWatchedAddresses().FindOne(ctx, bson.M{
		"user_id": sub,
		"primary": true,
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
			"primary": true,
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

// MarkServerDead marks all watched addresses for a given server as !primary.
// All affected users will receive new addresses the next time they request
// their address.
func (p *Promoter) MarkServerDead(server string) error {
	// Delete all addresses for that server which are not in use right now
	// and mark all the remaining addresses as !primary.
	// We do that within a single session for it to be ACID.
	return p.staticDB.Client().UseSession(p.staticBGCtx, func(sc mongo.SessionContext) error {
		_, err := p.staticColWatchedAddresses().DeleteMany(sc, bson.M{
			"$or": bson.A{
				bson.M{"user_id": bson.M{"$exists": false}},
				bson.M{"user_id": ""},
			},
			"server": server,
		})
		if err != nil {
			return err
		}
		_, err = p.staticColWatchedAddresses().UpdateMany(sc, bson.M{
			"server":  server,
			"primary": true,
		}, bson.M{
			"$set": bson.M{
				"primary": false,
			},
		})
		return err
	})
}

// SetPrimaryAddressInvalid marks the primary address for a user as !primary.
// The next time AddressForUser is called for that user, a new address will be
// returned.
func (p *Promoter) SetPrimaryAddressInvalid(sub string) error {
	// Set the primary address of a user to !primary. We use UpdateMany here
	// since a user should only ever have 1 primary address anyway. If
	// that's not the case we compensate this way.
	_, err := p.staticColWatchedAddresses().UpdateMany(context.Background(), bson.M{
		"user_id": sub,
		"primary": true,
	}, bson.M{
		"$set": bson.M{
			"primary": false,
		},
	})
	return err
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

// staticColTransactions returns the collection used to store transactions from
// skyd.
func (p *Promoter) staticColTransactions() *mongo.Collection {
	return p.staticDB.Collection(colTransactionsName)
}

// staticColWatchedAddresses returns the collection used to store watched
// addresses.
func (p *Promoter) staticColWatchedAddresses() *mongo.Collection {
	return p.staticDB.Collection(colWatchedAddressesName)
}

// staticColConfig returns the collection used to store config values.
func (p *Promoter) staticColConfig() *mongo.Collection {
	return p.staticDB.Collection(colConfigName)
}

// staticConversionRate returns the current conversion rate as configured in the
// database or initialises it.
func (p *Promoter) staticConversionRate() (*big.Rat, error) {
	// Find the setting.
	sr := p.staticColConfig().FindOne(p.staticBGCtx, bson.M{
		"_id": configIDConversionRate,
	})
	var ccr ConfigConversionRate
	err := sr.Decode(&ccr)

	// If it doesn't exist yet, set it to the default and return the default
	// conversion rate.
	if errors.Contains(err, mongo.ErrNoDocuments) {
		// If the config value isn't set yet, set it to the default.
		_, err := p.staticColConfig().InsertOne(p.staticBGCtx, bson.M{
			"_id":         configIDConversionRate,
			"numerator":   defaultConversionRate.Num().String(),
			"denominator": defaultConversionRate.Denom().String(),
		})
		if err != nil {
			return nil, err
		}
		return defaultConversionRate, nil
	}
	if err != nil {
		return nil, err
	}

	// Otherwise return the value from the db.
	cr, ok := ccr.Rat()
	if !ok {
		return nil, errors.New("failed to convert conversation rate to big.Rat")
	}
	return cr, nil
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
	for {
		select {
		case <-p.staticBGCtx.Done():
			return
		case <-t.C:
		}

		_, err := purger.Purge(p.staticBGCtx)
		if err != nil {
			p.staticLogger.WithTime(time.Now().UTC()).WithError(err).Error("Purging locks failed")
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

	// Check number of unused addresses.
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

// staticInsertTransactions inserts transactions into the transaction collection
// while ignoring any errors returned as a result of the txn being in the
// collection already.
func (p *Promoter) staticInsertTransactions(txns []interface{}) (n int, _ error) {
	imr, err := p.staticColTransactions().InsertMany(p.staticBGCtx, txns, options.InsertMany().SetOrdered(false))
	if imr != nil {
		n = len(imr.InsertedIDs)
	}
	if err == nil {
		return n, nil
	}

	// Check if error is a BulkWriteException. If not just return the error.
	bulkErr, isBulkErr := err.(mongo.BulkWriteException)
	if !isBulkErr {
		return n, err
	}
	// Otherwise we inspect the errors individually.
	var errs error
	for _, err := range bulkErr.WriteErrors {
		if !mongo.IsDuplicateKeyError(err) {
			errs = errors.Compose(errs, err)
		}
	}
	return n, errs
}
