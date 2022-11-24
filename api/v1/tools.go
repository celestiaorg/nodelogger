package api

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
)

func (a *RESTApiV1) getPagination(totalRows, pageNumber uint64) Pagination {
	totalPages := uint64(math.Ceil(float64(totalRows) / float64(a.rowsPerPage)))
	return Pagination{
		CurrentPage: pageNumber,
		TotalPages:  totalPages,
		TotalRows:   totalRows,
	}
}

func (a *RESTApiV1) getLimitOffsetFromHttpReq(req *http.Request) LimitOffset {

	page, _ := strconv.ParseUint(req.URL.Query().Get("page"), 10, 64)
	if page == 0 {
		page = 1
	}

	offset := (page - 1) * a.rowsPerPage

	return LimitOffset{
		Limit:  a.rowsPerPage,
		Offset: offset,
		Page:   page,
	}
}

func sendJSON(resp http.ResponseWriter, obj interface{}) error {

	data, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		http.Error(resp, "Internal Server Error: "+err.Error(), http.StatusInternalServerError)
		return err
	}

	// Allow CORS here By *
	resp.Header().Set("Access-Control-Allow-Origin", "*")
	resp.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	resp.Header().Set("Content-Type", "application/json")
	resp.Write(data)

	return nil
}
