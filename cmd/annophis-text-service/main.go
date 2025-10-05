package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	srv "github.com/GhentCDH/annophis-text-service/internal/server"
)

func main() {
	cfgPath := os.Getenv("CONFIG")
	if cfgPath == "" {
		cfgPath = "./config.json"
	}

	cfg, err := srv.LoadConfiguration(cfgPath)
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	s := srv.NewServer(cfg)
	router := srv.BuildRouter(s)

	addr := cfg.Port
	if addr == "" {
		addr = ":8080"
	}
	if addr[0] != ':' && !containsColon(addr) {
		addr = ":" + addr
	}

	httpSrv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	// graceful shutdown
	idle := make(chan struct{})
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
		close(idle)
	}()

	log.Printf("Listening on %s ...", addr)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
	<-idle
}

func containsColon(s string) bool {
	for _, r := range s {
		if r == ':' {
			return true
		}
	}
	return false
}
