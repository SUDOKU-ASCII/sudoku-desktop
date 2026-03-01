package core

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

func (b *Backend) startPACServer() {
	b.mu.Lock()
	if b.pacServer != nil || b.pacListener != nil {
		b.mu.Unlock()
		return
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.mu.Unlock()
		b.addLog("warn", "pac", fmt.Sprintf("start local PAC server failed: %v", err))
		return
	}
	addr := ln.Addr().(*net.TCPAddr)
	b.pacURL = fmt.Sprintf("http://127.0.0.1:%d/pac/custom", addr.Port)
	b.pacListener = ln
	mux := http.NewServeMux()
	mux.HandleFunc("/pac/custom", b.handlePACCustom)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}
	b.pacServer = srv
	b.mu.Unlock()

	go func() {
		_ = srv.Serve(ln)
	}()
}

func (b *Backend) stopPACServer() {
	b.mu.Lock()
	srv := b.pacServer
	ln := b.pacListener
	b.pacServer = nil
	b.pacListener = nil
	b.pacURL = ""
	b.mu.Unlock()

	if ln != nil {
		_ = ln.Close()
	}
	if srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func (b *Backend) handlePACCustom(w http.ResponseWriter, r *http.Request) {
	b.mu.RLock()
	enabled := b.cfg != nil && b.cfg.Routing.CustomRulesEnabled
	body := ""
	if b.cfg != nil {
		body = b.cfg.Routing.CustomRules
	}
	b.mu.RUnlock()

	body = strings.TrimSpace(body)
	if !enabled || body == "" {
		http.NotFound(w, r)
		return
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}
