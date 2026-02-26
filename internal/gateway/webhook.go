package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// WebhookGateway listens for HTTP POST requests and triggers the agent.
// Allows GHL, Zapier, or any external service to trigger ZBOT.
// Binds to 127.0.0.1 only — never exposed publicly without a tunnel.
type WebhookGateway struct {
	port    int
	secret  string // HMAC secret for request validation
	handler MessageHandler
	logger  *slog.Logger
}

// WebhookRequest is the expected JSON body for webhook calls.
type WebhookRequest struct {
	SessionID   string `json:"session_id"`
	Instruction string `json:"instruction"`
}

// WebhookResponse is the JSON response sent back.
type WebhookResponse struct {
	Reply string `json:"reply,omitempty"`
	Error string `json:"error,omitempty"`
}

// NewWebhookGateway creates a webhook listener.
func NewWebhookGateway(port int, secret string, handler MessageHandler, logger *slog.Logger) *WebhookGateway {
	return &WebhookGateway{
		port:    port,
		secret:  secret,
		handler: handler,
		logger:  logger,
	}
}

// Start begins listening for webhook POST requests. Blocks until ctx is cancelled.
func (w *WebhookGateway) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", w.handleWebhook)
	mux.HandleFunc("/health", func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
		rw.Write([]byte(`{"status":"ok"}`))
	})

	addr := fmt.Sprintf("127.0.0.1:%d", w.port)
	srv := &http.Server{Addr: addr, Handler: mux}

	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	w.logger.Info("webhook gateway listening", "addr", addr)
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// handleWebhook processes incoming POST /webhook requests.
func (w *WebhookGateway) handleWebhook(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate secret.
	if w.secret != "" {
		provided := r.Header.Get("X-ZBOT-Secret")
		if provided != w.secret {
			w.logger.Warn("webhook: invalid secret", "remote", r.RemoteAddr)
			http.Error(rw, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Parse body.
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		http.Error(rw, "read body failed", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req WebhookRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(rw, "invalid JSON", http.StatusBadRequest)
		return
	}

	if req.SessionID == "" {
		req.SessionID = "webhook"
	}
	if req.Instruction == "" {
		http.Error(rw, "instruction is required", http.StatusBadRequest)
		return
	}

	w.logger.Info("webhook request received",
		"session", req.SessionID,
		"instruction_len", len(req.Instruction),
		"remote", r.RemoteAddr,
	)

	// Call the agent handler.
	reply, err := w.handler(r.Context(), req.SessionID, "webhook", req.Instruction, nil)

	rw.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.logger.Error("webhook handler error", "err", err)
		rw.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(rw).Encode(WebhookResponse{Error: err.Error()})
		return
	}

	json.NewEncoder(rw).Encode(WebhookResponse{Reply: reply})
}
