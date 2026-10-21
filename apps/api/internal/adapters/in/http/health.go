package httpadapter

import "net/http"

// RegisterHealth registra la sonda de vida. La sonda de preparación, que
// comprueba dependencias, se añade con los contratos HTTP del paso 7.
func RegisterHealth(mux *http.ServeMux) {
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
}
