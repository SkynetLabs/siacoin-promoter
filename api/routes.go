package api

import (
	"net/http"

	"github.com/julienschmidt/httprouter"
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
