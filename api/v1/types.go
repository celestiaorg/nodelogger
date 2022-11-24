package api

import (
	"github.com/celestiaorg/nodelogger/database/metrics"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

type RESTApiV1 struct {
	router *mux.Router
	logger *zap.Logger

	metrics     *metrics.Metrics
	rowsPerPage uint64
}

type Pagination struct {
	CurrentPage uint64 `json:"current_page"`
	TotalPages  uint64 `json:"total_pages"`
	TotalRows   uint64 `json:"total_rows"`
}

type LimitOffset struct {
	Limit  uint64
	Offset uint64
	Page   uint64
}
