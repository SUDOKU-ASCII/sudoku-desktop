package core

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"
)

// customRuleServer serves the user's custom rules on loopback so the core can fetch them as
// one more rule source. It lives exactly as long as a session.
type customRuleServer struct {
	ln  net.Listener
	srv *http.Server
	url string
}

func startCustomRuleServer(rules string) (*customRuleServer, error) {
	body := strings.TrimSpace(rules)
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	// A stable port keeps the rule cache key stable across sessions.
	ln, err := net.Listen("tcp4", "127.0.0.1:10234")
	if err != nil {
		if ln, err = net.Listen("tcp4", "127.0.0.1:0"); err != nil {
			return nil, err
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/pac/custom", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(body))
	})
	s := &customRuleServer{
		ln:  ln,
		srv: &http.Server{Handler: mux, ReadHeaderTimeout: 3 * time.Second},
		url: "http://" + ln.Addr().String() + "/pac/custom",
	}
	go func() { _ = s.srv.Serve(ln) }()
	return s, nil
}

func (s *customRuleServer) URL() string { return s.url }

func (s *customRuleServer) stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	if err := s.srv.Shutdown(ctx); err != nil {
		_ = s.srv.Close()
	}
}
