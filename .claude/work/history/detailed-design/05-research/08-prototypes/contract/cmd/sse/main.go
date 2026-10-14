package main

import (
	"fmt"
	"net/http"
	"time"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/events", func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		h.Set("X-Accel-Buffering", "no")
		_ = rc.SetWriteDeadline(time.Time{}) // lift server WriteTimeout for this stream
		fmt.Fprint(w, "retry: 3000\n\n")
		_ = rc.Flush()
		t := time.NewTicker(1 * time.Second)
		defer t.Stop()
		i := 0
		for {
			select {
			case <-r.Context().Done():
				return
			case <-t.C:
				i++
				if i%3 == 0 {
					fmt.Fprint(w, ": keep-alive\n\n")
				} else {
					fmt.Fprintf(w, "id: %d\nevent: invalidate\ndata: {\"topic\":\"commits\",\"version\":%d}\n\n", i, i)
				}
				if err := rc.Flush(); err != nil {
					return
				}
			}
		}
	})
	srv := &http.Server{Addr: "127.0.0.1:9090", Handler: mux, WriteTimeout: 10 * time.Second, ReadHeaderTimeout: 5 * time.Second}
	_ = srv.ListenAndServe()
}
