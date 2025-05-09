package api

import (
	"errors"
	"net/http"

	"github.com/2DFS/2dfs-registry/v3/health"
)

var updater = health.NewStatusUpdater()

// init sets up the two endpoints to bring the service up and down
func init() {
	health.Register("manual_http_status", updater)
	http.HandleFunc("/debug/health/down", DownHandler)
	http.HandleFunc("/debug/health/up", UpHandler)
}

// DownHandler registers a manual_http_status that always returns an Error
func DownHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		updater.Update(errors.New("manual Check"))
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// UpHandler registers a manual_http_status that always returns nil
func UpHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		updater.Update(nil)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}
