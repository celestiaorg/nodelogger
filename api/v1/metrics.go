package api

import (
	"fmt"
	"net/http"

	"github.com/celestiaorg/leaderboard-backend/receiver"
	"github.com/gorilla/mux"
)

// GetBridgeNodes implements GET /metrics/nodes/bridge
func (a *RESTApiV1) GetBridgeNodes(resp http.ResponseWriter, req *http.Request) {

	limitOffset := a.getLimitOffsetFromHttpReq(req)

	nodesList, totalRows, err := a.metrics.GetNodesByType(receiver.BridgeNodeType, int(limitOffset.Offset), int(limitOffset.Limit))
	if err != nil {
		a.logger.Error(fmt.Sprintf("api `GetBridgeNodes`: %v", err))
		http.Error(resp, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	err = sendJSON(resp,
		map[string]interface{}{
			"pagination": a.getPagination(uint64(totalRows), limitOffset.Page),
			"rows":       nodesList,
		},
	)
	a.logger.Info(fmt.Sprintf("api call `GetBridgeNodes` %v ", req.URL.Path))
	a.logger.Debug(fmt.Sprintf("api call `GetBridgeNodes` limitOffset: %#v totalRows: %v", limitOffset, totalRows))

	if err != nil {
		a.logger.Error(fmt.Sprintf("sendJSON `GetBridgeNodes`: %v", err))
	}
}

// GetFullNodes implements GET /metrics/nodes/full
func (a *RESTApiV1) GetFullNodes(resp http.ResponseWriter, req *http.Request) {

	limitOffset := a.getLimitOffsetFromHttpReq(req)

	nodesList, totalRows, err := a.metrics.GetNodesByType(receiver.FullNodeType, int(limitOffset.Offset), int(limitOffset.Limit))
	if err != nil {
		a.logger.Error(fmt.Sprintf("api `GetFullNodes`: %v", err))
		http.Error(resp, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	err = sendJSON(resp,
		map[string]interface{}{
			"pagination": a.getPagination(uint64(totalRows), limitOffset.Page),
			"rows":       nodesList,
		},
	)
	a.logger.Info(fmt.Sprintf("api call `GetFullNodes` %v ", req.URL.Path))
	a.logger.Debug(fmt.Sprintf("api call `GetFullNodes` limitOffset: %#v totalRows: %v", limitOffset, totalRows))

	if err != nil {
		a.logger.Error(fmt.Sprintf("sendJSON `GetFullNodes`: %v", err))
	}
}

// GetLightNodes implements GET /metrics/nodes/light
func (a *RESTApiV1) GetLightNodes(resp http.ResponseWriter, req *http.Request) {

	limitOffset := a.getLimitOffsetFromHttpReq(req)

	nodesList, totalRows, err := a.metrics.GetNodesByType(receiver.LightNodeType, int(limitOffset.Offset), int(limitOffset.Limit))
	if err != nil {
		a.logger.Error(fmt.Sprintf("api `GetLightNodes`: %v", err))
		http.Error(resp, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	err = sendJSON(resp,
		map[string]interface{}{
			"pagination": a.getPagination(uint64(totalRows), limitOffset.Page),
			"rows":       nodesList,
		},
	)
	a.logger.Info(fmt.Sprintf("api call `GetLightNodes` %v ", req.URL.Path))
	a.logger.Debug(fmt.Sprintf("api call `GetLightNodes` limitOffset: %#v totalRows: %v", limitOffset, totalRows))

	if err != nil {
		a.logger.Error(fmt.Sprintf("sendJSON `GetLightNodes`: %v", err))
	}
}

// GetLightNodes implements GET /metrics/nodes/{id}
func (a *RESTApiV1) GetNodeById(resp http.ResponseWriter, req *http.Request) {

	id := mux.Vars(req)["id"]

	limitOffset := a.getLimitOffsetFromHttpReq(req)

	rows, totalRows, err := a.metrics.FindByNodeId(id, int(limitOffset.Offset), int(limitOffset.Limit))
	if err != nil {
		a.logger.Error(fmt.Sprintf("api `GetLightNodes`: %v", err))
		http.Error(resp, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if totalRows == 0 {
		http.Error(resp, fmt.Sprintf("no data found for node id `%s`", id), http.StatusNotFound)
		return
	}

	err = sendJSON(resp,
		map[string]interface{}{
			"pagination": a.getPagination(uint64(totalRows), limitOffset.Page),
			"rows":       rows,
		},
	)
	a.logger.Info(fmt.Sprintf("api call `FindNodeById` %v id: %v", req.URL.Path, id))
	a.logger.Debug(fmt.Sprintf("api call `FindNodeById` limitOffset: %#v totalRows: %v", limitOffset, totalRows))

	if err != nil {
		a.logger.Error(fmt.Sprintf("sendJSON `FindNodeById`: %v", err))
	}
}

// GetAllNodes implements GET /metrics/nodes
func (a *RESTApiV1) GetAllNodes(resp http.ResponseWriter, req *http.Request) {

	limitOffset := a.getLimitOffsetFromHttpReq(req)

	nodesList, totalRows, err := a.metrics.GetAllNodes(int(limitOffset.Offset), int(limitOffset.Limit))
	if err != nil {
		a.logger.Error(fmt.Sprintf("api `GetAllNodes`: %v", err))
		http.Error(resp, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	err = sendJSON(resp,
		map[string]interface{}{
			"pagination": a.getPagination(uint64(totalRows), limitOffset.Page),
			"rows":       nodesList,
		},
	)
	a.logger.Info(fmt.Sprintf("api call `GetAllNodes` %v ", req.URL.Path))
	a.logger.Debug(fmt.Sprintf("api call `GetAllNodes` limitOffset: %#v totalRows: %v", limitOffset, totalRows))

	if err != nil {
		a.logger.Error(fmt.Sprintf("sendJSON `GetAllNodes`: %v", err))
	}
}
