package promoter

import (
	"context"
	"sync"

	"github.com/sirupsen/logrus"
	"gitlab.com/SkynetLabs/skyd/node/api/client"
	"go.mongodb.org/mongo-driver/mongo"
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
		staticClient *mongo.Client
		staticDB     *mongo.Database
		staticLogger *logrus.Entry

		staticSkyd *client.Client

		ctx          context.Context
		bgCtx        context.Context
		threadCancel context.CancelFunc
		wg           sync.WaitGroup
	}
)

// New creates a new promoter from the given db credentials.
func New(ctx context.Context, skyd *client.Client, log *logrus.Entry, uri, username, password string) (*Promoter, error) {
	client, err := connect(ctx, log, uri, username, password)
	if err != nil {
		return nil, err
	}
	p := newPromoter(ctx, skyd, log, client)
	p.initBackgroundThreads(p.managedProcessAddressUpdate)
	return p, nil
}

// newPromoter creates a new promoter object from a given db client.
func newPromoter(ctx context.Context, skyd *client.Client, log *logrus.Entry, client *mongo.Client) *Promoter {
	// Grab database from client.
	database := client.Database(dbName)

	// Create a new context for background threads.
	bgCtx, cancel := context.WithCancel(ctx)

	// Create store.
	return &Promoter{
		bgCtx:        bgCtx,
		threadCancel: cancel,
		ctx:          ctx,
		staticClient: client,
		staticDB:     database,
		staticLogger: log,
		staticSkyd:   skyd,
	}
}

// Health returns some health information about the promoter.
func (p *Promoter) Health() Health {
	_, skydErr := p.staticSkyd.DaemonReadyGet()
	return Health{
		Database: p.staticClient.Ping(p.ctx, nil),
		Skyd:     skydErr,
	}
}

// initBackgroundThreads starts the background threads that the db requires.
func (p *Promoter) initBackgroundThreads(f updateFunc) {
	// Start watching the collection that contains the addresses we want
	// skyd to watch.
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.threadedAddressWatcher(p.bgCtx, f)
	}()
}
