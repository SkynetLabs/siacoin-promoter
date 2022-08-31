package api

import (
	"net/http"

	"github.com/julienschmidt/httprouter"
)

type (
	// HealthGET is the type returned by the /health endpoint.
	HealthGET struct {
		DBAlive   bool `json:"dbalive"`
		SkydAlive bool `json:"skydalive"`
	}
)

// buildHTTPRoutes registers the http routes with the httprouter.
func (api *API) buildHTTPRoutes() {
	api.staticRouter.GET("/health", api.healthGET)
}

// healthGET returns the status of the service
func (api *API) healthGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	var skydAlive bool
	if api.staticSkyd != nil {
		_, skydErr := api.staticSkyd.DaemonReadyGet()
		skydAlive = skydErr == nil
	}
	api.WriteJSON(w, HealthGET{
		DBAlive:   api.staticDB.Ping() == nil,
		SkydAlive: skydAlive,
	})
}
