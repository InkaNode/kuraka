package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/InkaNode/kuraka/internal/api"
	"github.com/InkaNode/kuraka/internal/config"
	"github.com/InkaNode/kuraka/internal/stalwart"
	"github.com/InkaNode/kuraka/internal/store"
	"github.com/InkaNode/kuraka/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version.Version)
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	sw := stalwart.New(cfg.StalwartURL, cfg.StalwartAPIKey)
	app, err := api.New(cfg, st, sw, log)
	if err != nil {
		return err
	}
	if token := app.SetupToken(); token != "" {
		log.Warn("setup required", "token", token, "endpoint", "POST /api/v1/setup")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	purgeDone := make(chan struct{})
	go func() {
		defer close(purgeDone)
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if _, err := st.PurgeExpiredSessions(ctx, now); err != nil && !errors.Is(err, context.Canceled) {
					log.Error("session purge failed", "error", err)
				}
			}
		}
	}()
	srv := &http.Server{Addr: cfg.Listen, Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second}
	serveErr := make(chan error, 1)
	go func() {
		log.Info("server listening", "address", cfg.Listen)
		serveErr <- srv.ListenAndServe()
	}()
	select {
	case err := <-serveErr:
		stop()
		<-purgeDone
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := srv.Shutdown(shutdownCtx)
		if err != nil {
			_ = srv.Close()
		}
		<-purgeDone
		if err != nil {
			return fmt.Errorf("HTTP shutdown: %w", err)
		}
		return nil
	}
}
