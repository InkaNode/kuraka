package jobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/InkaNode/kuraka/internal/auth"
	"github.com/InkaNode/kuraka/internal/config"
	"github.com/InkaNode/kuraka/internal/migrate"
	"github.com/InkaNode/kuraka/internal/stalwart"
	"github.com/InkaNode/kuraka/internal/store"
)

type fakeStalwart struct {
	mu                           sync.Mutex
	account                      stalwart.Account
	created, deleted             int
	descriptions                 []string
	getErr, createErr, deleteErr error
}

func (f *fakeStalwart) GetAccount(context.Context, string) (stalwart.Account, error) {
	return f.account, f.getErr
}
func (f *fakeStalwart) CreateAppPassword(_ context.Context, _, desc string) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return "", "", f.createErr
	}
	f.created++
	f.descriptions = append(f.descriptions, desc)
	return fmt.Sprintf("app%d", f.created), "target-private-secret", nil
}
func (f *fakeStalwart) DeleteAppPassword(ctx context.Context, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	f.deleted++
	return f.deleteErr
}

func fixture(t *testing.T) (*Runner, *fakeStalwart, store.MigrationJob) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/jobs.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	tenant, err := st.CreateTenant(ctx, "Tenant")
	if err != nil {
		t.Fatal(err)
	}
	domain, err := st.CreateDomain(ctx, tenant.ID, "example.test", "d1")
	if err != nil {
		t.Fatal(err)
	}
	box, err := auth.NewBox(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	enc, err := box.Seal([]byte("source-private-secret"))
	if err != nil {
		t.Fatal(err)
	}
	job, err := st.CreateMigrationJob(ctx, store.MigrationJob{DomainID: domain.ID, AccountID: "a1", SourceHost: "source.test", SourcePort: 993, SourceTLS: "tls", SourceUser: "old-user", SourceSecretEnc: enc})
	if err != nil {
		t.Fatal(err)
	}
	sw := &fakeStalwart{account: stalwart.Account{ID: "a1", DomainID: "d1", Name: "user", Email: "user@example.test"}}
	r := New(st, sw, box, config.Config{MailHost: "mail.test", MailTLSSkipVerify: true, MigrationWorkers: 2}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return r, sw, job
}

func claimed(t *testing.T, r *Runner) store.MigrationJob {
	t.Helper()
	job, err := r.st.ClaimNextMigrationJob(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func TestRunnerSuccessAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			r, sw, _ := fixture(t)
			job := claimed(t, r)
			r.runFn = func(_ context.Context, src, dst migrate.Endpoint, report func(migrate.Progress)) (migrate.Progress, error) {
				if src.Password != "source-private-secret" || src.Host != "source.test" || src.Port != 993 || src.TLS != "tls" || src.Username != "old-user" {
					t.Fatalf("source: %+v", src)
				}
				if dst.Host != "mail.test" || dst.Port != 993 || dst.TLS != "tls" || !dst.SkipVerify || dst.Username != "user@example.test" || dst.Password != "target-private-secret" {
					t.Fatalf("destination: %+v", dst)
				}
				for i := 1; i <= 10; i++ {
					report(migrate.Progress{MessagesTotal: 10, MessagesCopied: i})
				}
				stored, err := r.st.GetMigrationJob(context.Background(), job.ID)
				if err != nil || stored.MessagesCopied != 1 {
					t.Fatalf("progress not throttled: %+v %v", stored, err)
				}
				p := migrate.Progress{FoldersTotal: 1, FoldersDone: 1, MessagesTotal: 10, MessagesCopied: 9, MessagesSkipped: 1}
				if fail {
					return p, errors.New("failed source-private-secret target-private-secret")
				}
				return p, nil
			}
			r.execute(context.Background(), job)
			stored, err := r.st.GetMigrationJob(context.Background(), job.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := "done"
			if fail {
				want = "failed"
			}
			if stored.Status != want || stored.SourceSecretEnc != "" || stored.MessagesCopied != 9 || stored.MessagesSkipped != 1 || stored.FinishedAt == nil {
				t.Fatalf("finished job: %+v", stored)
			}
			if strings.Contains(stored.Error, "private-secret") {
				t.Fatal("secret in job error")
			}
			if sw.created != 1 || sw.deleted != 1 || sw.descriptions[0] != fmt.Sprintf("migration #%d", job.ID) {
				t.Fatal("credential lifecycle failed")
			}
		})
	}
}

func TestRunnerCancellation(t *testing.T) {
	r, sw, _ := fixture(t)
	job := claimed(t, r)
	r.runFn = func(ctx context.Context, _, _ migrate.Endpoint, _ func(migrate.Progress)) (migrate.Progress, error) {
		if err := r.st.RequestCancelMigration(ctx, job.ID); err != nil {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			return migrate.Progress{}, ctx.Err()
		case <-time.After(5 * time.Second):
			t.Fatal("worker did not notice cancellation")
			return migrate.Progress{}, nil
		}
	}
	r.execute(context.Background(), job)
	stored, err := r.st.GetMigrationJob(context.Background(), job.ID)
	if err != nil || stored.Status != "cancelled" || stored.SourceSecretEnc != "" || sw.deleted != 1 {
		t.Fatalf("cancel: %+v %v", stored, err)
	}
}

func TestRunnerCleanupOnEarlyFailure(t *testing.T) {
	for _, mode := range []string{"decrypt", "lookup", "password", "domain", "revoke"} {
		t.Run(mode, func(t *testing.T) {
			r, sw, _ := fixture(t)
			job := claimed(t, r)
			r.runFn = func(context.Context, migrate.Endpoint, migrate.Endpoint, func(migrate.Progress)) (migrate.Progress, error) {
				return migrate.Progress{}, nil
			}
			switch mode {
			case "decrypt":
				job.SourceSecretEnc = "corrupt"
			case "lookup":
				sw.getErr = errors.New("lookup")
			case "password":
				sw.createErr = errors.New("password")
			case "domain":
				sw.account.DomainID = "foreign"
			case "revoke":
				sw.deleteErr = errors.New("revoke")
			}
			r.execute(context.Background(), job)
			stored, err := r.st.GetMigrationJob(context.Background(), job.ID)
			if err != nil || stored.Status != "failed" || stored.SourceSecretEnc != "" {
				t.Fatalf("early failure: %+v %v", stored, err)
			}
			if mode == "decrypt" && sw.deleted != 1 {
				t.Fatal("app password leaked on decryption failure")
			}
		})
	}
}

func TestRunnerRecoveryPoolAndShutdown(t *testing.T) {
	r, sw, first := fixture(t)
	interrupted := claimed(t, r)
	for range 3 {
		copy := first
		copy.ID = 0
		if _, err := r.st.CreateMigrationJob(context.Background(), copy); err != nil {
			t.Fatal(err)
		}
	}
	var active, peak atomic.Int32
	entered := make(chan struct{}, 4)
	r.runFn = func(ctx context.Context, _, _ migrate.Endpoint, _ func(migrate.Progress)) (migrate.Progress, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- struct{}{}
		<-ctx.Done()
		return migrate.Progress{}, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)
	for range 2 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("workers not started")
		}
	}
	r.Start(ctx) // must not re-run recovery or create another pool
	cancel()
	r.Wait()
	if peak.Load() != 2 || sw.created != 2 || sw.deleted != 2 {
		t.Fatalf("pool/cleanup: peak %d create %d delete %d", peak.Load(), sw.created, sw.deleted)
	}
	stored, err := r.st.GetMigrationJob(context.Background(), interrupted.ID)
	if err != nil || stored.Status != "failed" || stored.Error != "interrupted" || stored.SourceSecretEnc != "" {
		t.Fatalf("recovery: %+v %v", stored, err)
	}
	jobs, err := r.st.ListMigrationJobs(context.Background(), first.DomainID, 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range jobs {
		if job.Status == "running" {
			t.Fatal("job left running at shutdown")
		}
	}
}
