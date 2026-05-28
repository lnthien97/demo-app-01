// Package main is a tiny HTTP "hello" service used as a CI/CD demo target.
//
// Endpoints:
//   GET /          -> "Hello from <hostname> (v<version>)"
//   GET /health    -> 200 "ok"   (used by Kubernetes liveness/readiness probes)
//   GET /version   -> JSON {version, commit, hostname}
//
// Behaviour is controlled by environment variables:
//   PORT     (default 8080)         - TCP port to listen on
//   VERSION  (default "dev")        - shown in /version and /
//   COMMIT   (default "unknown")    - git SHA, injected at build time
//   MESSAGE  (default "Hello")      - greeting prefix, sourced from a ConfigMap
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// These are populated at build time via -ldflags "-X main.version=... -X main.commit=...".
// They can also be overridden at runtime via the VERSION / COMMIT env vars.
var (
	version = "dev"
	commit  = "unknown"
)

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func main() {
	// Env vars take precedence over ldflags-injected defaults.
	version = env("VERSION", version)
	commit = env("COMMIT", commit)

	var (
		port        = env("PORT", "8080")
		message     = env("MESSAGE", "Hello")
		hostname, _ = os.Hostname()
	)

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(message + " from " + hostname + " (v" + version + ")\n"))
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"version":  version,
			"commit":   commit,
			"hostname": hostname,
		})
	})

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Graceful shutdown so rolling updates don't drop in-flight requests.
	go func() {
		log.Printf("listening on :%s (version=%s commit=%s)", port, version, commit)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("shutdown signal received, draining…")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
	log.Println("bye")
}

// logRequests is a tiny middleware that logs method, path, status, duration.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, sw.status, time.Since(start))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
