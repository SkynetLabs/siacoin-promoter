package api

import (
	"net/http"

	"github.com/julienschmidt/httprouter"
)

type (
	HealthGET struct {
		DBAlive bool `json:"dbalive"`
	}
)

func (api *API) buildHTTPRoutes() {
	api.staticRouter.GET("/health", api.healthGET)
}

// healthGET returns the status of the service
func (api *API) healthGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	api.WriteJSON(w, HealthGET{
		DBAlive: api.staticDB.Ping() == nil,
	})
}
