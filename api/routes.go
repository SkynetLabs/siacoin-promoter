package api

import (
	"net/http"

	"github.com/julienschmidt/httprouter"
	"gitlab.com/NebulousLabs/errors"
	"go.mongodb.org/mongo-driver/mongo"
	"go.sia.tech/siad/types"
)

type (
	// HealthGET is the type returned by the /health endpoint.
	HealthGET struct {
		DBAlive   bool `json:"dbalive"`
		SkydAlive bool `json:"skydalive"`
	}

	// UserAddressPOST is the type returned by the /address endpoint.
	UserAddressPOST struct {
		Address types.UnlockHash `json:"address"`
	}
)

// buildHTTPRoutes registers the http routes with the httprouter.
func (api *API) buildHTTPRoutes() {
	api.staticRouter.GET("/health", api.healthGET)
	api.staticRouter.POST("/address", api.userAddressPOST)
	api.staticRouter.POST("/dead/:servername", api.deadServerPOST)
}

// healthGET returns the status of the service
func (api *API) healthGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	ph := api.staticPromoter.Health()
	api.WriteJSON(w, HealthGET{
		DBAlive:   ph.Database == nil,
		SkydAlive: ph.Skyd == nil,
	})
}

// userAddressPOST is the handler for the /address endpoint.
func (api *API) userAddressPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Get sub from accounts service.
	sub, err := api.staticPromoter.SubFromAuthorizationHeader(req.Header)
	if err != nil {
		api.WriteError(w, err, http.StatusBadRequest)
		return
	}

	// Get address.
	addr, err := api.staticPromoter.AddressForUser(req.Context(), sub)
	if err != nil {
		api.WriteError(w, err, http.StatusInternalServerError)
		return
	}
	api.WriteJSON(w, UserAddressPOST{
		Address: addr,
	})
}

// deadServerPOST is the handler for the /dead/:servername endpoint.
func (api *API) deadServerPOST(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	server := ps.ByName("servername")
	if server == "" {
		api.WriteError(w, errors.New("name of server wasn't provided"), http.StatusBadRequest)
		return
	}

	err := api.staticPromoter.MarkServerDead(server)
	if errors.Contains(err, mongo.ErrNoDocuments) {
		api.WriteError(w, errors.AddContext(err, "no server matches the given name"), http.StatusNotFound)
		return
	}
	if err != nil {
		api.WriteError(w, errors.AddContext(err, "failed to mark server dead"), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
