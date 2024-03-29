package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/celestiaorg/leaderboard-backend/receiver"
	"github.com/celestiaorg/nodelogger/database/models"
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

// GetNodeById implements GET /metrics/nodes/{id}
func (a *RESTApiV1) GetNodeById(resp http.ResponseWriter, req *http.Request) {

	id := mux.Vars(req)["id"]

	limitOffset := a.getLimitOffsetFromHttpReq(req)

	rows, totalRows, err := a.metrics.FindByNodeId(id, int(limitOffset.Offset), int(limitOffset.Limit))
	if err != nil {
		a.logger.Error(fmt.Sprintf("api `GetNodeById`: %v", err))
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
	a.logger.Info(fmt.Sprintf("api call `GetNodeById` %v id: %v", req.URL.Path, id))
	a.logger.Debug(fmt.Sprintf("api call `GetNodeById` limitOffset: %#v totalRows: %v", limitOffset, totalRows))

	if err != nil {
		a.logger.Error(fmt.Sprintf("sendJSON `GetNodeById`: %v", err))
	}
}

// GetNodeByIdAtNetworkHeight implements GET /metrics/nodes/{id}/height/{height}
// GetNodeByIdAtNetworkHeight implements GET /metrics/nodes/{id}/height/{height}/{height_end}  Search into a range of heights
func (a *RESTApiV1) GetNodeByIdAtNetworkHeight(resp http.ResponseWriter, req *http.Request) {

	id := mux.Vars(req)["id"]

	heightStr := mux.Vars(req)["height"]
	height, err := strconv.ParseUint(heightStr, 10, 64)
	if err != nil {
		a.logger.Error(fmt.Sprintf("api `GetNodeByIdAtNetworkHeight`: %v", err))
		http.Error(resp, "malformed height value", http.StatusBadRequest)
		return
	}

	heightEnd := uint64(0)
	heightEndStr := mux.Vars(req)["height_end"]

	if heightEndStr != "" {

		heightEnd, err = strconv.ParseUint(heightEndStr, 10, 64)
		if err != nil {
			a.logger.Error(fmt.Sprintf("api `GetNodeByIdAtNetworkHeight`: %v", err))
			http.Error(resp, "malformed height_end value", http.StatusBadRequest)
			return
		}
	}

	var rows []models.CelestiaNode
	if heightEnd != 0 {
		rows, err = a.metrics.FindByNodeIdAtNetworkHeightRange(id, height, heightEnd)
	} else {
		rows, err = a.metrics.FindByNodeIdAtNetworkHeight(id, height)
	}

	if err != nil {
		a.logger.Error(fmt.Sprintf("api `GetNodeByIdAtNetworkHeight`: %v", err))
		http.Error(resp, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// cut the rows if there are too many of them
	if len(rows) > 100 {
		rows = rows[:100]
	}

	err = sendJSON(resp,
		map[string]interface{}{
			"rows": rows,
		},
	)
	a.logger.Info(fmt.Sprintf("api call `GetNodeByIdAtNetworkHeight` %v id: %v", req.URL.Path, id))
	a.logger.Debug(fmt.Sprintf("api call `GetNodeByIdAtNetworkHeight` totalRows: %v", len(rows)))

	if err != nil {
		a.logger.Error(fmt.Sprintf("sendJSON `GetNodeByIdAtNetworkHeight`: %v", err))
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
