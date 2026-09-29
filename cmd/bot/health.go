package main

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/grysha11/poe-tg-tracker/internal/gatewayclient"
)

const (
	livenessWindow  = 3 * time.Minute
	readinessWindow = 3 * time.Minute
)

type health struct {
	loopAt   atomic.Int64
	pollOKAt atomic.Int64
	gw       *gatewayclient.Client
}

func (h *health) beat()   { h.loopAt.Store(time.Now().UnixNano()) }
func (h *health) pollOK() { h.pollOKAt.Store(time.Now().UnixNano()) }

func since(ts *atomic.Int64) time.Duration {
	return time.Since(time.Unix(0, ts.Load()))
}

func (h *health) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		if since(&h.loopAt) > livenessWindow {
			http.Error(w, "poll loop stalled", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if since(&h.pollOKAt) > readinessWindow {
			http.Error(w, "no successful getUpdates recently", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := h.gw.Ready(ctx); err != nil {
			http.Error(w, "gateway not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}
