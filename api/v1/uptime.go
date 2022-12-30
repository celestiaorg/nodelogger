package api

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
)

// GetNodeUptimeById implements GET /uptime/nodes/{id}
func (a *RESTApiV1) GetNodeUptimeById(resp http.ResponseWriter, req *http.Request) {

	id := mux.Vars(req)["id"]

	uptime, err := a.metrics.GetNodeUpTime(id)
	if err != nil {
		a.logger.Error(fmt.Sprintf("api `GetNodeUptimeById`: %v", err))
		http.Error(resp, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	err = sendJSON(resp,
		map[string]interface{}{
			"uptime":  uptime,
			"node_id": id,
		},
	)
	a.logger.Info(fmt.Sprintf("api call `GetNodeUptimeById` %v id: %v", req.URL.Path, id))

	if err != nil {
		a.logger.Error(fmt.Sprintf("sendJSON `GetNodeUptimeById`: %v", err))
	}
}
