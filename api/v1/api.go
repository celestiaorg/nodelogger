package api

import (
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/celestiaorg/nodelogger/database/metrics"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

func path(endpoint string) string {
	return fmt.Sprintf("/api/v1%s", endpoint)
}

func NewRESTApiV1(mt *metrics.Metrics, logger *zap.Logger) *RESTApiV1 {

	rowsPerPageStr := os.Getenv("API_ROWS_PER_PAGE")
	rowsPerPage := uint64(100)
	if rowsPerPageStr == "" {
		num, err := strconv.ParseUint(rowsPerPageStr, 10, 32)
		if err == nil {
			rowsPerPage = num
		}
	}

	api := &RESTApiV1{
		router:      mux.NewRouter(),
		logger:      logger,
		metrics:     mt,
		rowsPerPage: rowsPerPage,
	}

	api.router.HandleFunc("/", api.IndexPage).Methods("GET")
	// api.router.HandleFunc("/ui", api.UI).Methods("GET")

	api.router.HandleFunc(path("/metrics/nodes"), api.GetAllNodes).Methods("GET")
	api.router.HandleFunc(path("/metrics/nodes/bridge"), api.GetBridgeNodes).Methods("GET")
	api.router.HandleFunc(path("/metrics/nodes/full"), api.GetFullNodes).Methods("GET")
	api.router.HandleFunc(path("/metrics/nodes/light"), api.GetLightNodes).Methods("GET")
	api.router.HandleFunc(path("/metrics/nodes/{id}"), api.GetNodeById).Methods("GET")

	api.router.HandleFunc(path("/uptime/nodes/{id}"), api.GetNodeUptimeById).Methods("GET")

	return api
}

func (a *RESTApiV1) Serve(addr string) error {

	if addr == "" {
		addr = ":8090"
	}

	http.Handle("/", a.router)

	a.logger.Info(fmt.Sprintf("serving on %s", addr))
	return http.ListenAndServe(addr, a.router)
}

func (a *RESTApiV1) GetAllAPIs() []string {

	list := []string{}

	a.router.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
		apiPath, err := route.GetPathTemplate()
		if err == nil {
			list = append(list, apiPath)
		}
		return err
	})

	return list
}
