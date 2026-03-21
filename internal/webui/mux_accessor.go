package webui

import "net/http"

// Mux returns the underlying HTTP mux for registering additional routes.
func (s *Server) Mux() *http.ServeMux {
	return s.mux
}
