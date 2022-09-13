package promoter

import (
	"context"
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

		staticSkyd *client.Client

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
func New(ctx context.Context, skyd *client.Client, log *logrus.Entry, uri, username, password, domain, db string) (*Promoter, error) {
	client, err := connect(ctx, log, uri, username, password)
	if err != nil {
		return nil, err
	}
	p, err := newPromoter(ctx, skyd, log, client, domain, db)
	if err != nil {
		return nil, err
	}
	p.initBackgroundThreads(p.managedProcessAddressUpdate)
	return p, nil
}

// newPromoter creates a new promoter object from a given db client.
func newPromoter(ctx context.Context, skyd *client.Client, log *logrus.Entry, client *mongo.Client, domain, db string) (*Promoter, error) {
	// Grab database from client.
	database := client.Database(db)

	// Create a new context for background threads.
	bgCtx, cancel := context.WithCancel(ctx)

	// Create store.
	p := &Promoter{
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
	// TODO: Fetch all txns with credited == false.

	// TODO: Credit the promoter. NOTE: This needs to be idempotent so we
	// include a txn ID in the request. Requesting a credit with the same
	// txn ID should still only credit it once.

	// TODO: Update the credited txns in the database as credited = true.
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
