package test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/SkynetLabs/siacoin-promoter/api"
	"github.com/SkynetLabs/siacoin-promoter/promoter"
	"github.com/julienschmidt/httprouter"
	"github.com/sirupsen/logrus"
	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/SkynetLabs/skyd/node/api/client"
)

// newTestPromoter creates a Promoter instance for testing.
func newTestPromoter(skyd *client.Client, name, accountsAddr string) (*promoter.Promoter, error) {
	username := "admin"
	// nolint:gosec // Disable gosec since these are only test credentials.
	password := "aO4tV5tC1oU3oQ7u"
	uri := "mongodb://localhost:37017"
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	ac := promoter.NewAccountsClient(accountsAddr)
	return promoter.New(context.Background(), ac, skyd, logrus.NewEntry(logger), uri, username, password, name, name)
}

// Tester is a pair of an API and a client to talk to that API for testing.
// Multiple testers will always talk to the same underlying database but have
// their APIs listen on different ports.
type Tester struct {
	*api.PromoterClient
	staticAPI         *api.API
	staticAccountsSrv *httptest.Server

	shutDown    chan struct{}
	shutDownErr error
}

// Close shuts the tester down gracefully.
func (t *Tester) Close() error {
	defer t.staticAccountsSrv.Close()

	if err := t.staticAPI.Shutdown(context.Background()); err != nil {
		return err
	}
	<-t.shutDown
	if errors.Contains(t.shutDownErr, http.ErrServerClosed) {
		return nil // Ignore shutdown error
	}
	return t.shutDownErr
}

func newAccountsMock() *http.Server {
	router := httprouter.New()
	accountsSrv := httptest.NewServer(router)
}

// newTester creates a new, ready-to-go tester.
func newTester(skydClient *client.Client, server string) (*Tester, error) {
	// Mock accounts service.
	accountsSrv := newAccountsMock()

	// Create discard logger.
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	db, err := newTestPromoter(skydClient, server, "")
	if err != nil {
		return nil, err
	}

	// Create API.
	a, err := api.New(logrus.NewEntry(logger), db, 0)
	if err != nil {
		return nil, err
	}

	// Create client pointing to API.
	addr := fmt.Sprintf("http://%s", a.Address())
	client := api.NewClient(addr)
	tester := &Tester{
		PromoterClient:    client,
		staticAccountsSrv: accountsSrv,
		staticAPI:         a,
		shutDown:          make(chan struct{}),
	}

	// Start listening.
	go func() {
		tester.shutDownErr = tester.staticAPI.ListenAndServe()
		close(tester.shutDown)
	}()
	return tester, nil
}
