package http

import (
	"net/http"

	"github.com/vibewarden/vibewarden/internal/apispec"
)

// RegisterDocsRoute registers the public OpenAPI spec endpoint on mux.
// GET /_vibewarden/api/docs returns the embedded openapi.yaml with
// Content-Type: application/yaml. No authentication is required for this route.
func RegisterDocsRoute(mux *http.ServeMux) {
	mux.HandleFunc("GET /_vibewarden/api/docs", serveOpenAPISpec)
}

// serveOpenAPISpec writes the embedded OpenAPI YAML specification to the response.
func serveOpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(apispec.Spec)
}
