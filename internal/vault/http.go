package vault

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Handler provides HTTP endpoints for the vault.
// Mount at /api/vault/ on the main router.
type Handler struct {
	vault  *Vault
	userID string // single-user mode for now; multi-tenant adds auth middleware
}

// NewHandler creates vault HTTP handlers.
func NewHandler(v *Vault, userID string) *Handler {
	return &Handler{vault: v, userID: userID}
}

// Register mounts vault routes on a mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/vault/secrets", h.handleSecrets)
	mux.HandleFunc("/api/vault/secrets/", h.handleSecretByKey)
}

// GET /api/vault/secrets → list keys
// POST /api/vault/secrets → create/update a secret
func (h *Handler) handleSecrets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		keys, err := h.vault.List(r.Context(), h.userID)
		if err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Return keys with masked preview
		type secretInfo struct {
			Key     string `json:"key"`
			Version int    `json:"version,omitempty"`
		}
		infos := make([]secretInfo, 0, len(keys))
		for _, k := range keys {
			s, _ := h.vault.store.Get(r.Context(), h.userID, k)
			info := secretInfo{Key: k}
			if s != nil {
				info.Version = s.Version
			}
			infos = append(infos, info)
		}
		jsonResp(w, map[string]any{"secrets": infos, "count": len(infos)})

	case http.MethodPost:
		var req struct {
			Key      string            `json:"key"`
			Value    string            `json:"value"`
			Metadata map[string]string `json:"metadata,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if req.Key == "" || req.Value == "" {
			jsonError(w, "key and value are required", http.StatusBadRequest)
			return
		}

		if err := h.vault.Put(r.Context(), h.userID, req.Key, req.Value, req.Metadata); err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResp(w, map[string]any{
			"status": "stored",
			"key":    req.Key,
			"masked": MaskValue(req.Value),
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET /api/vault/secrets/{key} → get a secret (masked by default, ?raw=true for plaintext)
// DELETE /api/vault/secrets/{key} → delete a secret
func (h *Handler) handleSecretByKey(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/api/vault/secrets/")
	if key == "" {
		jsonError(w, "key is required in path", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		val, err := h.vault.Get(r.Context(), h.userID, key)
		if err != nil {
			jsonError(w, fmt.Sprintf("secret %q not found", key), http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("raw") == "true" {
			jsonResp(w, map[string]any{"key": key, "value": val})
		} else {
			jsonResp(w, map[string]any{"key": key, "masked": MaskValue(val)})
		}

	case http.MethodDelete:
		if err := h.vault.Delete(r.Context(), h.userID, key); err != nil {
			jsonError(w, err.Error(), http.StatusNotFound)
			return
		}
		jsonResp(w, map[string]any{"status": "deleted", "key": key})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func jsonResp(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
