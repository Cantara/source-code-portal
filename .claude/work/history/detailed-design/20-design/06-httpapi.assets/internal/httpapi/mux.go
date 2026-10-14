package httpapi

import "net/http"

// Mux is exactly oapi-codegen v2.8.0's generated ServeMux interface (verified: HandleFunc plus http.Handler).
// *http.ServeMux and *routes.Recorder satisfy it.
type Mux interface {
	HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request))
	http.Handler
}

// Mounter is implemented by every API-owning feature.
type Mounter interface{ Mount(m Mux) }

// MountAll mounts each Mounter on m in order. A nil Mounter is a wiring bug and panics at startup.
func MountAll(m Mux, ms ...Mounter) {
	for _, x := range ms {
		x.Mount(m)
	}
}
