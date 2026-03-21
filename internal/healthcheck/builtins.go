package healthcheck

import (
	"database/sql"
	"fmt"
	"net/http"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// PostgresCheck creates a CheckFunc that pings a Postgres database.
func PostgresCheck(connStr string) CheckFunc {
	return func() Check {
		db, err := sql.Open("postgres", connStr)
		if err != nil {
			return Check{
				Name:   "postgres",
				Status: "down",
				Latency: 0,
				Message: err.Error(),
			}
		}
		defer db.Close()

		start := time.Now()
		err = db.Ping()
		latency := time.Since(start)

		if err != nil {
			return Check{
				Name:   "postgres",
				Status: "down",
				Latency: latency,
				Message: err.Error(),
			}
		}

		return Check{
			Name:    "postgres",
			Status:  "ok",
			Latency: latency,
		}
	}
}

// OllamaCheck creates a CheckFunc that checks the Ollama API.
func OllamaCheck(baseURL string) CheckFunc {
	return func() Check {
		url := baseURL + "/api/tags"
		start := time.Now()
		resp, err := http.Get(url)
		latency := time.Since(start)

		if err != nil {
			return Check{
				Name:   "ollama",
				Status: "down",
				Latency: latency,
				Message: err.Error(),
			}
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return Check{
				Name:   "ollama",
				Status: "down",
				Latency: latency,
				Message: fmt.Sprintf("unexpected status code: %d", resp.StatusCode),
			}
		}

		return Check{
			Name:    "ollama",
			Status:  "ok",
			Latency: latency,
		}
	}
}

// DiskCheck creates a CheckFunc that checks disk space.
func DiskCheck(path string, minFreeGB float64) CheckFunc {
	return func() Check {
		var stat syscall.Statfs_t
		err := syscall.Statfs(path, &stat)
		if err != nil {
			return Check{
				Name:   "disk",
				Status: "down",
				Latency: 0,
				Message: err.Error(),
			}
		}

		// Calculate free space in GB
		freeSpaceGB := float64(stat.Bavail) * float64(stat.Bsize) / (1024 * 1024 * 1024)

		status := "ok"
		if freeSpaceGB < minFreeGB {
			status = "degraded"
		}

		return Check{
			Name:    "disk",
			Status:  status,
			Latency: 0,
			Message: fmt.Sprintf("free space: %.2fGB", freeSpaceGB),
		}
	}
}