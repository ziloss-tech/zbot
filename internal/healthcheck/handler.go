package healthcheck

import (
	"encoding/json"
	"net/http"
	"time"
)

// Handler returns an HTTP handler function that responds with the health check results.
func Handler(checker Checker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		results := checker.RunAll()
		overallStatus := "ok"
		timestamp := time.Now().Format(time.RFC3339)

		for _, result := range results {
			if result.Status == "down" {
				overallStatus = "down"
				break
			}
			if result.Status == "degraded" && overallStatus != "down" {
				overallStatus = "degraded"
			}
		}

		response := struct {
			Status  string  `json:"status"`
			Checks  []Check `json:"checks"`
			Timestamp string `json:"timestamp"`
		}{
			Status:  overallStatus,
			Checks:  results,
			Timestamp: timestamp,
		}

		w.Header().Set("Content-Type", "application/json")
		if overallStatus == "down" {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		encoder := json.NewEncoder(w)
		if err := encoder.Encode(response); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}