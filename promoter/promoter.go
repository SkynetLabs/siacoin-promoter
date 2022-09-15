package promoter

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	lock "github.com/square/mongo-lock"
	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/SkynetLabs/skyd/node/api/client"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.sia.tech/siad/build"
	"go.sia.tech/siad/types"
)

type (
	// Health contains health information about the promoter. Namely the
	// database and skyd. If everything is ok all fields are 'nil'.
	// Otherwise the corresponding fields will contain an error.
	Health struct {
		Database error
		Skyd     error
	}

	// Promoter is a wrapper around a skyd and a database client. It makes
	// sure that skyd watches all the siacoin addresses it is supposed to
	// and is capable of adding new addresses to watch and removing old
	// ones. It can also track the incoming funds that users have sent to
	// their assigned addresses.
	Promoter struct {
		staticDB           *mongo.Database
		staticLogger       *logrus.Entry
		staticServerDomain string

		// The lock client is used for locing the creation of new
		// addresses within the watched addresses collection. Only if an
		// exclusive lock is acquired, new addresses are allowed to be
		// inserted or the length of the collection be requested.
		staticLockClient *lock.Client

		staticAccounts *AccountsClient
		staticSkyd     *client.Client

		staticCtx          context.Context
		staticBGCtx        context.Context
		staticThreadCancel context.CancelFunc
		staticWG           sync.WaitGroup
	}
)

var (
	// txnPollInterval is the interval for polling for transactions from skyd.
	txnPollInterval = build.Select(build.Var{
		Dev:      time.Minute,
		Standard: 10 * time.Minute,
		Testing:  5 * time.Second,
	}).(time.Duration)
)

// New creates a new promoter from the given db credentials.
func New(ctx context.Context, ac *AccountsClient, skyd *client.Client, log *logrus.Entry, uri, username, password, domain, db string) (*Promoter, error) {
	client, err := connect(ctx, log, uri, username, password)
	if err != nil {
		return nil, err
	}
	p, err := newPromoter(ctx, ac, skyd, log, client, domain, db)
	if err != nil {
		return nil, err
	}
	p.initBackgroundThreads(p.managedProcessAddressUpdate)
	return p, nil
}

// newPromoter creates a new promoter object from a given db client.
func newPromoter(ctx context.Context, ac *AccountsClient, skyd *client.Client, log *logrus.Entry, client *mongo.Client, domain, db string) (*Promoter, error) {
	// Grab database from client.
	database := client.Database(db)

	// Create a new context for background threads.
	bgCtx, cancel := context.WithCancel(ctx)

	// Create store.
	p := &Promoter{
		staticAccounts:     ac,
		staticBGCtx:        bgCtx,
		staticThreadCancel: cancel,
		staticCtx:          ctx,
		staticDB:           database,
		staticLogger:       log,
		staticServerDomain: domain,
		staticSkyd:         skyd,
	}

	// Create lock client.
	lockClient := lock.NewClient(p.staticColLocks())
	err := lockClient.CreateIndexes(ctx)
	if err != nil {
		return nil, errors.Compose(err, p.Close())
	}
	p.staticLockClient = lockClient

	// Kick off creation of addresses in non-testing builds. This is not
	// really necessary but it will prevent the first user ever from getting
	// an error when trying to fetch an address in production.
	if build.Release != "testing" {
		p.staticWG.Add(1)
		go func() {
			p.threadedRegenerateAddresses()
			p.staticWG.Done()
		}()
	}
	return p, nil
}

// Health returns some health information about the promoter.
func (p *Promoter) Health() Health {
	_, skydErr := p.staticSkyd.DaemonReadyGet()
	return Health{
		Database: p.staticDB.Client().Ping(p.staticCtx, nil),
		Skyd:     skydErr,
	}
}

// initBackgroundThreads starts the background threads that the db requires.
func (p *Promoter) initBackgroundThreads(f updateFunc) {
	// Start watching the collection that contains the addresses we want
	// skyd to watch.
	p.staticWG.Add(1)
	go func() {
		defer p.staticWG.Done()
		p.threadedAddressWatcher(p.staticBGCtx, f)
	}()
	p.staticWG.Add(1)
	go func() {
		defer p.staticWG.Done()
		p.threadedPruneLocks()
	}()
	p.staticWG.Add(1)
	go func() {
		defer p.staticWG.Done()
		p.threadedPollTransactions()
	}()
	p.staticWG.Add(1)
	go func() {
		defer p.staticWG.Done()
		p.threadedCreditTransactions()
	}()
}

// staticAddrDiff returns a diff of addresses that describes which addresses
// need to be added and removed from skyd to match the state of the database.
// Every skyd needs to watch all addresses from the watched address collection
// in the database.
func (p *Promoter) staticAddrDiff(ctx context.Context) (toAdd []WatchedAddress, toRemove []types.UnlockHash, _ error) {
	// Fetch addresses.
	skydAddrs, err := p.staticWatchedSkydAddresses()
	if err != nil {
		return nil, nil, err
	}
	dbAddrs, err := p.staticWatchedDBAddresses(ctx)
	if err != nil {
		return nil, nil, err
	}

	// Turn slices into maps.
	skydAddrsMap := make(map[types.UnlockHash]struct{}, len(skydAddrs))
	for _, addr := range skydAddrs {
		skydAddrsMap[addr] = struct{}{}
	}
	dbAddrsMap := make(map[types.UnlockHash]WatchedAddress, len(dbAddrs))
	for _, addr := range dbAddrs {
		dbAddrsMap[addr.Address] = addr
	}

	// Create the diff.
	for _, addr := range dbAddrs {
		_, exists := skydAddrsMap[addr.Address]
		if !exists {
			toAdd = append(toAdd, addr)
		}
	}
	for _, addr := range skydAddrs {
		_, exists := dbAddrsMap[addr]
		if !exists {
			toRemove = append(toRemove, addr)
		}
	}
	return
}

// threadedCreditTransactions continuously polls the db for uncreditted txns and
// notifies the credit promoter about them.
func (p *Promoter) threadedCreditTransactions() {
	t := time.NewTicker(txnPollInterval)
	defer t.Stop()
LOOP:
	for {
		select {
		case <-p.staticBGCtx.Done():
			return
		case <-t.C:
		}

		// Get credit conversion rate at the beginning of this iteration.
		cr, err := p.staticConversionRate()
		if err != nil {
			p.staticLogger.WithError(err).Error("Failed to fetch siacoin conversion rate")
			continue // retry later
		}

		// Loop over txns one-by-one.
		for {
			// Fetch an transaction that the credit system doesn't know
			// about yet.
			currentTime := time.Now().UTC()
			sr := p.staticColTransactions().FindOneAndUpdate(p.staticBGCtx, bson.M{
				"credited": false,
				"credited_at": bson.M{
					"$lt": currentTime.Add(-txnPollInterval),
				},
			}, bson.M{
				"$set": bson.M{
					"credited_at": currentTime,
				},
			})
			if errors.Contains(sr.Err(), mongo.ErrNoDocuments) {
				continue LOOP // no more txns in this iteration
			}
			if sr.Err() != nil {
				p.staticLogger.WithError(sr.Err()).Error("Failed to fetch another uncredited txn")
				continue LOOP // db failure, try again later
			}

			// Decode txn.
			var txn Transaction
			if err := sr.Decode(&txn); err != nil {
				build.Critical(fmt.Sprintf("failed to decode txn: %v", err))
				p.staticLogger.WithError(err).Error("Failed to decode txn")
				continue // try next txn
			}

			// Fetch the user for the txn.
			sr = p.staticColWatchedAddresses().FindOne(p.staticBGCtx, bson.M{
				"_id": txn.Address,
			})
			if errors.Contains(sr.Err(), mongo.ErrNoDocuments) {
				build.Critical("Address for txn doesn't exist - this should never happen")
				p.staticLogger.WithError(sr.Err()).Error("Address for txn doesn't exist")
				continue // try next
			}
			if sr.Err() != nil {
				p.staticLogger.WithError(sr.Err()).Error("Failed to fetch address for txn")
				continue LOOP // db failure, try again later
			}
			var wa WatchedAddress
			if err := sr.Decode(&wa); err != nil {
				build.Critical(fmt.Sprintf("failed to decode address: %v", err))
				p.staticLogger.WithError(sr.Err()).Error("Failed to decode address for txn")
				continue // try next
			}

			// Parse the amount to credit.
			var amt types.Currency
			if _, err := fmt.Sscan(txn.Value, &amt); err != nil {
				p.staticLogger.WithError(sr.Err()).Error("Failed to parse txn amount")
				continue // try next
			}

			// Send txn to credit system.
			if err := p.staticCreditTxn(wa.UserSub, txn.TxnID, amt, cr); err != nil {
				p.staticLogger.WithError(sr.Err()).Error("Failed to submit txn to credit system")
				continue LOOP // something is wrong with the credit system - skip iteration
			}

			// Upon success mark it as credited.
			_, err := p.staticColTransactions().UpdateOne(p.staticBGCtx, bson.M{
				"_id": txn.TxnID,
			}, bson.M{
				"$set": bson.M{
					"credited": true,
				},
			})
			if err != nil {
				p.staticLogger.WithError(err).Error("Failed to credit txn")
				continue // try next txn
			}
		}
	}
}

// threadedPollTransactions continuously polls skyd for transactions related to
// watched addresses and writes them to the DB.
func (p *Promoter) threadedPollTransactions() {
	t := time.NewTicker(txnPollInterval)
	defer t.Stop()
	for {
		select {
		case <-p.staticBGCtx.Done():
			return
		case <-t.C:
		}
		p.staticLogger.WithTime(time.Now().UTC()).Info("Starting to poll transactions from skyd")

		// Get used addresses.
		c, err := p.staticColWatchedAddresses().Find(p.staticBGCtx, bson.M{
			"user_id": bson.M{
				"$exists": true,
				"$ne":     "",
			},
		})
		if err != nil {
			p.staticLogger.WithError(err).Error("Failed to fetch used addresses")
			continue
		}

		// For each one get the related txns from skyd and save them to
		// the db.
		var nAddresssInserted, nTxnsInserted int
		// Get addresses.
		var was []WatchedAddress
		if err := c.All(p.staticBGCtx, &was); err != nil {
			p.staticLogger.WithError(err).Error("Failed to decode address")
			continue // try next address
		}

		for _, wa := range was {
			// Fetch related txns from skyd.
			txns, err := p.staticTxnsByAddress(wa.Address)
			if err != nil {
				p.staticLogger.WithError(err).Error("Failed to fetch txns from skyd")
				break // skyd is offline, wait for next interval
			}

			// Insert txns.
			n, err := p.staticInsertTransactions(txns)
			nTxnsInserted += n
			if err != nil {
				p.staticLogger.WithError(err).Error("Failed to insert txns into db")
				break // db is malfunctioning, wait for next interval
			}
			nAddresssInserted++
		}
		p.staticLogger.WithTime(time.Now().UTC()).Infof("Inserted %v transactions for %v addresses", nTxnsInserted, nAddresssInserted)
	}
}
