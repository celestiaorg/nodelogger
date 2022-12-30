package api

import (
	"fmt"
	"net/http"
	"os"
)

// IndexPage implements GET /
func (a *RESTApiV1) IndexPage(resp http.ResponseWriter, req *http.Request) {

	homeHTML := "Ciao, this is `NodeLogger` :) <p>"
	allAPIs := a.GetAllAPIs()
	for _, a := range allAPIs {
		homeHTML += fmt.Sprintf(`<a href="%s">%s</a><br />`, a, a)
	}

	resp.Header().Set("Content-Type", "text/html; charset=utf-8")
	resp.Write([]byte(homeHTML))
}

func (a *RESTApiV1) UI(resp http.ResponseWriter, req *http.Request) {

	rootPath := os.Getenv("EXEC_PATH")
	if rootPath == "" {
		rootPath = "./"
	}

	http.FileServer(http.Dir(rootPath)).ServeHTTP(resp, req)
}
