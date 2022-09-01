package promoter

import (
	"context"
	"sync"

	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/mongo"
)

type (
	// Database is a wrapper for the connection to the database and
	// abstracts all interactions with the database.
	Promoter struct {
		staticClient *mongo.Client
		staticDB     *mongo.Database
		staticLogger *logrus.Entry

		staticColWatchedAddresses *mongo.Collection

		ctx          context.Context
		bgCtx        context.Context
		threadCancel context.CancelFunc

		wg sync.WaitGroup
	}
)

// New creates a new promoter from the given db credentials.
func New(ctx context.Context, log *logrus.Entry, uri, username, password string) (*Promoter, error) {
	client, err := connect(ctx, log, uri, username, password)
	if err != nil {
		return nil, err
	}
	p := newPromoter(ctx, log, client)
	p.initBackgroundThreads(p.managedProcessAddressUpdate)
	return p, nil
}

// newPromoter creates a new promoter object from a given db client.
func newPromoter(ctx context.Context, log *logrus.Entry, client *mongo.Client) *Promoter {
	// Grab database and collections for convenience fields.
	database := client.Database(dbName)
	watchedAddrs := database.Collection(colWatchedAddressesName)

	// Create a new context for background threads.
	bgCtx, cancel := context.WithCancel(ctx)

	// Create store.
	return &Promoter{
		bgCtx:                     bgCtx,
		threadCancel:              cancel,
		ctx:                       ctx,
		staticClient:              client,
		staticColWatchedAddresses: watchedAddrs,
		staticDB:                  database,
		staticLogger:              log,
	}
}

// initBackgroundThreads starts the background threads that the db requires.
func (db *Promoter) initBackgroundThreads(f updateFunc) {
	// Start watching the collection that contains the addresses we want
	// skyd to watch.
	db.wg.Add(1)
	go func() {
		defer db.wg.Done()
		db.threadedAddressWatcher(db.bgCtx, f)
	}()
}
