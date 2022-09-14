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
}

// healthGET returns the status of the service
func (api *API) healthGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	ph := api.staticPromoter.Health()
	api.WriteJSON(w, HealthGET{
		DBAlive:   ph.Database == nil,
		SkydAlive: ph.Skyd == nil,
	})
}

func (api *API) userAddressPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Get sub from accounts service.

}
