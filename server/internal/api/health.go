package api

import "net/http"

// handleHealthz is the container health check. It must not leak configuration,
// tokens or receiver data.
func (a *App) handleHealthz(w http.ResponseWriter, r *http.Request) {
	a.respond(w, r, http.StatusOK, map[string]string{"status": "ok"})
}
