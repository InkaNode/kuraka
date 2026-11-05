package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/InkaNode/kuraka/internal/api"
	"github.com/InkaNode/kuraka/internal/auth"
	"github.com/InkaNode/kuraka/internal/config"
	"github.com/InkaNode/kuraka/internal/jobs"
	"github.com/InkaNode/kuraka/internal/provision"
	"github.com/InkaNode/kuraka/internal/stalwart"
	"github.com/InkaNode/kuraka/internal/store"
	"github.com/InkaNode/kuraka/internal/version"
	"github.com/InkaNode/kuraka/web"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		return healthcheck(os.Getenv(config.EnvListen))
	}
	if len(os.Args) > 1 && os.Args[1] == "bundle" {
		if len(os.Args) != 4 {
			return errors.New("usage: kuraka bundle <name> <host>")
		}
		name, host := os.Args[2], os.Args[3]
		if len(name) > 48 || !regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`).MatchString(name) || !config.ValidHostname(host) || !strings.Contains(host, ".") {
			return errors.New("invalid bundle name or host")
		}
		bundle, err := provision.Bundle(name, host, provision.Options{SystemPassword: os.Getenv("KURAKA_BUNDLE_SYSTEM_PASSWORD")})
		if err != nil {
			return err
		}
		return zipToTar(os.Stdout, bundle.Zip)
	}
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
	box, err := auth.NewBox(cfg.SecretKeyHex)
	if err != nil {
		return err
	}
	runner := jobs.New(st, sw, box, cfg, log)
	runner.Start(ctx)
	metricsDone := jobs.StartMetrics(ctx, st, sw, cfg, log)
	updatesDone := app.StartUpdateChecks(ctx)
	activationDone := app.StartHelpdeskActivation(ctx)
	defer func() { stop(); runner.Wait(); <-metricsDone; <-updatesDone; <-activationDone }()
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
	app.WithUI(web.FS())
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

func zipToTar(w io.Writer, data []byte) error {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	tw := tar.NewWriter(w)
	for _, file := range z.File {
		if err := tw.WriteHeader(&tar.Header{Name: file.Name, Mode: int64(file.Mode().Perm()), Size: int64(file.UncompressedSize64)}); err != nil {
			return err
		}
		r, err := file.Open()
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, r)
		closeErr := r.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return tw.Close()
}
