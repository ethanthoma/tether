package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"
)

const authTimeout = 5 * time.Minute

// Run on the desktop, not atlas: Google only allows loopback redirects for
// desktop clients, and the resulting refresh token is then stored via secretspec.
func RunAuth(cfg *Config) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("auth: listen: %w", err)
	}
	defer listener.Close()
	redirect := fmt.Sprintf("http://127.0.0.1:%d", listener.Addr().(*net.TCPAddr).Port)

	codes := make(chan string, 1)
	failures := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if reason := r.URL.Query().Get("error"); reason != "" {
			fmt.Fprintf(w, "consent denied: %s — you can close this tab", reason)
			failures <- fmt.Errorf("auth: consent denied: %s", reason)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "no code in callback", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, "tether is authorized — you can close this tab")
		codes <- code
	})
	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			failures <- err
		}
	}()
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()

	log.Printf("open this url to authorize tether:\n\n%s\n", googleConsentURL(cfg, redirect))

	var code string
	select {
	case code = <-codes:
	case err := <-failures:
		return err
	case <-time.After(authTimeout):
		return fmt.Errorf("auth: timed out waiting for consent")
	}

	refreshToken, err := googleExchangeCode(cfg, code, redirect)
	if err != nil {
		return err
	}
	fmt.Printf("\nrefresh token (store as TETHER_GOOGLE_REFRESH_TOKEN):\n\n%s\n", refreshToken)
	return nil
}
