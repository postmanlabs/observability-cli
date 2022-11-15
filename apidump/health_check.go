package apidump

import (
	"fmt"
	"github.com/gorilla/mux"
	"net/http"
)

// Handles health check requests for the Docker Extension.
// Returns 200 OK by default.
func handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status": "ok"}`))
}

func startHealthCheckServer(port int) error {
	router := mux.NewRouter()
	router.HandleFunc("/health", handleHealthCheck)
	return http.ListenAndServe(fmt.Sprintf(":%d", port), router)
}
