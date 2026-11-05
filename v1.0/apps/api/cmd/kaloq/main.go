package main

import (
	"context"
	"encoding/base64"
	"kaloq/v1/internal/server"
	"kaloq/v1/internal/store"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	log.SetFlags(0)
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	ctx := context.Background()
	pool, err := store.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		return err
	}
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	if command == "migrate" {
		return store.Migrate(ctx, pool, os.Getenv("MIGRATIONS_DIR"))
	}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("TOTP_ENCRYPTION_KEY"))
	if err != nil || len(key) != 32 {
		return &configError{"TOTP_ENCRYPTION_KEY must be base64 32 bytes"}
	}
	origin := os.Getenv("PUBLIC_ORIGIN")
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.Path != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))) {
		return &configError{"PUBLIC_ORIGIN must be an HTTPS origin (localhost HTTP allowed)"}
	}
	s := &server.Server{Pool: pool, Key: key, Origin: origin, CookieSecure: u.Scheme == "https", CookiePath: os.Getenv("COOKIE_PATH"), WebDir: os.Getenv("WEB_DIR"), TrustProxyLoopback: os.Getenv("TRUST_PROXY_LOOPBACK") == "true"}
	if command == "bootstrap" {
		return s.Bootstrap(ctx, os.Getenv("BOOTSTRAP_LOGIN"), os.Getenv("BOOTSTRAP_NAME"), os.Getenv("BOOTSTRAP_PASSWORD"))
	}
	if command != "serve" {
		return &configError{"unknown command"}
	}
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:4102"
	}
	h := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	stop, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-stop.Done()
		c, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		h.Shutdown(c)
	}()
	log.Print("Kaloq v1 listening on ", addr)
	err = h.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

type configError struct{ s string }

func (e *configError) Error() string { return e.s }
