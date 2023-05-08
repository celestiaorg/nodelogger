package api

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
)

// GetNodeVersionsById implements GET /versions/nodes/{id}
func (a *RESTApiV1) GetNodeVersionsById(resp http.ResponseWriter, req *http.Request) {

	id := mux.Vars(req)["id"]

	rows, err := a.metrics.GetNodesVersions(id)
	if err != nil {
		a.logger.Error(fmt.Sprintf("api `GetNodeVersionsById`: %v", err))
		http.Error(resp, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if len(rows) == 0 {
		a.logger.Info(fmt.Sprintf("api `GetNodeVersionsById`: %v", err))
		http.Error(resp, "no version data found", http.StatusNotFound)
		return
	}

	err = sendJSON(resp, rows)
	a.logger.Info(fmt.Sprintf("api call `GetNodeVersionsById` %v id: %v", req.URL.Path, id))

	if err != nil {
		a.logger.Error(fmt.Sprintf("sendJSON `GetNodeVersionsById`: %v", err))
	}
}
