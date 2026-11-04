// Package jobs runs persisted mailbox migrations in a bounded worker pool.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/InkaNode/kuraka/internal/auth"
	"github.com/InkaNode/kuraka/internal/config"
	"github.com/InkaNode/kuraka/internal/migrate"
	"github.com/InkaNode/kuraka/internal/stalwart"
	"github.com/InkaNode/kuraka/internal/store"
)

type Stalwart interface {
	CreateAppPassword(context.Context, string, string) (string, string, error)
	DeleteAppPassword(context.Context, string, string) error
	GetAccount(context.Context, string) (stalwart.Account, error)
}

type Runner struct {
	st    *store.Store
	sw    Stalwart
	box   *auth.Box
	cfg   config.Config
	log   *slog.Logger
	runFn func(context.Context, migrate.Endpoint, migrate.Endpoint, func(migrate.Progress)) (migrate.Progress, error)
	once  sync.Once
	wg    sync.WaitGroup
}

func New(st *store.Store, sw Stalwart, box *auth.Box, cfg config.Config, log *slog.Logger) *Runner {
	if log == nil {
		log = slog.Default()
	}
	return &Runner{st: st, sw: sw, box: box, cfg: cfg, log: log, runFn: migrate.Run}
}

// Start recovers interrupted work once, then starts workers and returns.
func (r *Runner) Start(ctx context.Context) {
	r.once.Do(func() {
		if _, err := r.st.FailInterruptedMigrations(ctx, time.Now()); err != nil {
			r.log.Error("migration recovery failed")
			return
		}
		workers := r.cfg.MigrationWorkers
		if workers == 0 {
			workers = config.DefaultMigrationWorkers
		}
		workers = max(1, min(workers, 8))
		for range workers {
			r.wg.Add(1)
			go func() { defer r.wg.Done(); r.worker(ctx) }()
		}
	})
}

// Wait lets the server keep the store open until credentials are revoked and
// final states are persisted during graceful shutdown.
func (r *Runner) Wait() { r.wg.Wait() }

func (r *Runner) worker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		job, err := r.st.ClaimNextMigrationJob(ctx, time.Now())
		if err == nil {
			r.execute(ctx, job)
			continue
		}
		if !errors.Is(err, store.ErrNotFound) && ctx.Err() == nil {
			r.log.Error("migration claim failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Runner) execute(parent context.Context, job store.MigrationJob) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	var appID, appSecret, sourceSecret string
	status, errText := "failed", "Migration failed"
	defer func() {
		if appID != "" {
			cleanup, stop := context.WithTimeout(context.WithoutCancel(parent), 30*time.Second)
			if err := r.sw.DeleteAppPassword(cleanup, job.AccountID, appID); err != nil && !errors.Is(err, stalwart.ErrNotFound) {
				status = "failed"
				errText = "App password revocation failed"
				r.log.Error("migration credential revocation failed", "job_id", job.ID)
			}
			stop()
		}
		finish, stop := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
		defer stop()
		if err := r.st.FinishMigrationJob(finish, job.ID, status, errText, time.Now()); err != nil {
			r.log.Error("migration finalization failed", "job_id", job.ID)
		}
	}()
	if r.cfg.MailHost == "" {
		errText = "Mail host is not configured"
		return
	}
	account, err := r.sw.GetAccount(ctx, job.AccountID)
	if err != nil {
		errText = "Mailbox lookup failed"
		return
	}
	domain, err := r.st.GetDomain(ctx, job.DomainID)
	if err != nil || account.DomainID != domain.StalwartID {
		errText = "Mailbox domain mismatch"
		return
	}
	email := account.Email
	if email == "" {
		email = account.Name + "@" + domain.Name
	}
	appID, appSecret, err = r.sw.CreateAppPassword(ctx, job.AccountID, fmt.Sprintf("migration #%d", job.ID))
	if err != nil {
		errText = "App password creation failed"
		return
	}
	plain, err := r.box.Open(job.SourceSecretEnc)
	if err != nil {
		errText = "Source credentials could not be decrypted"
		return
	}
	sourceSecret = string(plain)
	// External sources always verify TLS; only our own mail host may share its lab setting.
	sameHost := strings.EqualFold(job.SourceHost, r.cfg.MailHost)
	src := migrate.Endpoint{Host: job.SourceHost, Port: job.SourcePort, TLS: job.SourceTLS, SkipVerify: sameHost && r.cfg.MailTLSSkipVerify, Username: job.SourceUser, Password: sourceSecret}
	dst := migrate.Endpoint{Host: r.cfg.MailHost, Port: 993, TLS: "tls", SkipVerify: r.cfg.MailTLSSkipVerify, Username: email, Password: appSecret}
	requested, err := r.st.IsMigrationCancelRequested(ctx, job.ID)
	if err != nil {
		errText = "Cancellation lookup failed"
		return
	}
	if requested {
		status, errText = "cancelled", ""
		return
	}
	monitorDone := make(chan struct{})
	stopMonitor := make(chan struct{})
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopMonitor:
				return
			case <-ticker.C:
				requested, err := r.st.IsMigrationCancelRequested(ctx, job.ID)
				if err != nil {
					if ctx.Err() == nil {
						r.log.Error("migration cancellation lookup failed", "job_id", job.ID)
					}
					continue
				}
				if requested {
					cancel()
					return
				}
			}
		}
	}()
	last := time.Time{}
	report := func(p migrate.Progress) {
		if time.Since(last) < time.Second {
			return
		}
		last = time.Now()
		if err := r.st.UpdateMigrationProgress(ctx, job.ID, progress(p)); err != nil && ctx.Err() == nil {
			r.log.Error("migration progress update failed", "job_id", job.ID)
		}
	}
	p, runErr := r.runFn(ctx, src, dst, report)
	close(stopMonitor)
	<-monitorDone
	finishCtx, stop := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer stop()
	if err := r.st.UpdateMigrationProgress(finishCtx, job.ID, progress(p)); err != nil {
		r.log.Error("migration final progress failed", "job_id", job.ID)
	}
	requested, err = r.st.IsMigrationCancelRequested(finishCtx, job.ID)
	if err != nil {
		errText = "Cancellation lookup failed"
		return
	}
	if requested {
		status, errText = "cancelled", ""
		return
	}
	if runErr == nil && parent.Err() == nil {
		status, errText = "done", ""
		return
	}
	if parent.Err() != nil {
		errText = "interrupted"
		return
	}
	if runErr != nil {
		errText = runErr.Error()
		for _, secret := range []string{sourceSecret, appSecret, job.SourceSecretEnc} {
			if secret != "" {
				errText = strings.ReplaceAll(errText, secret, "[redacted]")
			}
		}
	}
}

func progress(p migrate.Progress) store.MigrationProgress {
	return store.MigrationProgress{FoldersTotal: p.FoldersTotal, FoldersDone: p.FoldersDone, MessagesTotal: p.MessagesTotal, MessagesCopied: p.MessagesCopied, MessagesSkipped: p.MessagesSkipped}
}
