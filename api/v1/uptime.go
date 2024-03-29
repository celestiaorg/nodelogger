package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"gorm.io/gorm"
)

// GetNodeUptimeById implements GET /uptime/nodes/{id}
func (a *RESTApiV1) GetNodeUptimeById(resp http.ResponseWriter, req *http.Request) {

	id := mux.Vars(req)["id"]

	uptime, err := a.metrics.GetNodeUpTime(id)
	if err != nil {
		if errors.Is(gorm.ErrRecordNotFound, err) {
			a.logger.Info(fmt.Sprintf("api `GetNodeUptimeById`: %v", err))
			http.Error(resp, err.Error(), http.StatusNotFound)
			return
		}
		a.logger.Error(fmt.Sprintf("api `GetNodeUptimeById`: %v", err))
		http.Error(resp, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	nodeRecords, _, err := a.metrics.FindByNodeId(id, 0, 1)
	if err == nil && len(nodeRecords) == 0 {
		err = fmt.Errorf("node data not found")
		a.logger.Info(fmt.Sprintf("api `GetNodeUptimeById`: %v", err))
		http.Error(resp, err.Error(), http.StatusNotFound)
		return
	}
	if err != nil {
		a.logger.Error(fmt.Sprintf("api `GetNodeUptimeById`: %v", err))
		http.Error(resp, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	err = sendJSON(resp,
		map[string]interface{}{
			"uptime":    uptime,
			"node_id":   id,
			"node_type": nodeRecords[0].NodeType.String(),
		},
	)
	a.logger.Info(fmt.Sprintf("api call `GetNodeUptimeById` %v id: %v", req.URL.Path, id))

	if err != nil {
		a.logger.Error(fmt.Sprintf("sendJSON `GetNodeUptimeById`: %v", err))
	}
}
