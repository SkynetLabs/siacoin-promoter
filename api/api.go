package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/SkynetLabs/siacoin-promoter/database"
	"github.com/julienschmidt/httprouter"
	"github.com/sirupsen/logrus"
)

type (
	// API manages the http API and all of its routes.
	API struct {
		staticDB     *database.Database
		staticLog    *logrus.Entry
		staticRouter *httprouter.Router
		staticServer *http.Server
	}

	// Error is the error type returned by the API in case the status code
	// is not a 2xx code.
	Error struct {
		Message string `json:"message"`
	}

	// errorWrap is a helper type for converting an `error` struct to JSON.
	errorWrap struct {
		Message string `json:"message"`
	}
)

// Error implements the error interface for the Error type. It returns only the
// Message field.
func (err Error) Error() string {
	return err.Message
}

// New creates a new API with the given logger and database.
func New(log *logrus.Entry, db *database.Database, port int) (*API, error) {
	router := httprouter.New()
	router.RedirectTrailingSlash = true
	api := &API{
		staticDB:     db,
		staticLog:    log,
		staticRouter: router,
		staticServer: &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: router,

			// Set low timeouts since we expect to only talk to this
			// service on the same machine.
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       10 * time.Second,
		},
	}
	api.buildHTTPRoutes()
	return api, nil
}

// ListenAndServe starts the API. To unblock this call Shutdown.
func (api *API) ListenAndServe() error {
	return api.staticServer.ListenAndServe()
}

// Shutdown gracefully shuts down the API.
func (api *API) Shutdown(ctx context.Context) error {
	return api.staticServer.Shutdown(ctx)
}

// WriteError an error to the API caller.
func (api *API) WriteError(w http.ResponseWriter, err error, code int) {
	api.staticLog.WithError(err).WithField("statuscode", code)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	encodingErr := json.NewEncoder(w).Encode(errorWrap{Message: err.Error()})
	if encodingErr != nil {
		api.staticLog.WithError(encodingErr).Error("Failed to encode error response object")
	}
}

// WriteJSON writes the object to the ResponseWriter. If the encoding fails, an
// error is written instead. The Content-Type of the response header is set
// accordingly.
func (api *API) WriteJSON(w http.ResponseWriter, obj interface{}) {
	api.staticLog.Debug("WriteJSON:", obj)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(obj)
	if err != nil {
		api.staticLog.WithError(err).Error("Failed to encode response object")
	}
}
