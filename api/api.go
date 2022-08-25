package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/SkynetLabs/siacoin-promoter/database"
	"github.com/julienschmidt/httprouter"
	"github.com/sirupsen/logrus"
)

type (
	API struct {
		staticDB     *database.Database
		staticRouter *httprouter.Router
		staticLog    *logrus.Entry
	}

	// errorWrap is a helper type for converting an `error` struct to JSON.
	errorWrap struct {
		Message string `json:"message"`
	}
)

func New(log *logrus.Entry, db *database.Database) (*API, error) {
	router := httprouter.New()
	router.RedirectTrailingSlash = true
	api := &API{
		staticDB:     db,
		staticLog:    log,
		staticRouter: router,
	}
	api.buildHTTPRoutes()
	return api, nil
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

// ListenAndServe starts the API server on the given port.
func (api *API) ListenAndServe(port int) error {
	api.staticLog.WithField("port", port).Info("Listening for incoming connections")
	return http.ListenAndServe(fmt.Sprintf(":%d", port), api.staticRouter)
}
