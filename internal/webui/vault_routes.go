package webui

import "net/http"

// VaultRegistrar is any handler that can register vault routes.
type VaultRegistrar interface {
	Register(mux *http.ServeMux)
}

// RegisterVaultHandler mounts vault REST API endpoints on the web server.
func (s *Server) RegisterVaultHandler(h VaultRegistrar) {
	h.Register(s.mux)
}
