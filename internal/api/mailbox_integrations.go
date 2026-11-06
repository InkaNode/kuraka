package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/InkaNode/kuraka/internal/authz"
	"github.com/InkaNode/kuraka/internal/libredesk"
	"github.com/InkaNode/kuraka/internal/stalwart"
	"github.com/InkaNode/kuraka/internal/store"
)

func (s *Server) requireMailHost() error {
	if s.cfg.MailHost == "" {
		return &apiError{status: 409, code: "mail_host_not_configured", message: "Mail host is not configured"}
	}
	return nil
}

func linkJSON(link store.HelpdeskLink) map[string]any {
	return map[string]any{"id": link.ID, "domain_id": link.DomainID, "account_id": link.AccountID, "helpdesk_id": link.HelpdeskID, "inbox_id": link.InboxID, "created_at": link.CreatedAt}
}

func (s *Server) mailboxHelpdesks(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, a, err := s.authorizedMailbox(r)
	if err != nil {
		return nil, err
	}
	links, err := s.st.ListHelpdeskLinks(r.Context(), d.ID, a.ID)
	out := make([]any, 0, len(links))
	for _, link := range links {
		out = append(out, linkJSON(link))
	}
	return out, err
}

func cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
}

func (s *Server) connectHelpdesk(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, a, err := s.authorizedMailbox(r)
	if err != nil {
		return nil, err
	}
	// Groups have no credentials in Stalwart, so they cannot get an app password.
	if a.Kind == stalwart.KindGroup {
		return nil, sharedMailbox
	}
	if err := s.requireMailHost(); err != nil {
		return nil, err
	}
	var in struct {
		HelpdeskID int64  `json:"helpdesk_id"`
		InboxName  string `json:"inbox_name"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	h, err := s.st.GetHelpdesk(r.Context(), in.HelpdeskID)
	if err != nil {
		return nil, err
	}
	if h.TenantID != d.TenantID {
		return nil, authz.ErrNotFound
	}
	if h.Status == "pending" {
		return nil, helpdeskNotReady
	}
	links, err := s.st.ListHelpdeskLinks(r.Context(), d.ID, a.ID)
	if err != nil {
		return nil, err
	}
	for _, link := range links {
		if link.HelpdeskID == h.ID {
			return nil, conflict("Mailbox is already connected")
		}
	}
	client, err := s.helpdeskClient(h)
	if err != nil {
		return nil, err
	}
	id, secret, err := s.sw.CreateAppPassword(r.Context(), a.ID, "libredesk:"+h.Name)
	if err != nil {
		return nil, integrationFailure("App password creation failed", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		ctx, cancel := cleanupContext(r.Context())
		defer cancel()
		if err := s.sw.DeleteAppPassword(ctx, a.ID, id); err != nil {
			s.log.Error("helpdesk rollback: app password revocation failed", "account_id", a.ID, "app_password_id", id)
		}
	}()
	email := a.Email
	if email == "" {
		email = a.Name + "@" + d.Name
	}
	display := a.Description
	if display == "" {
		display = a.Name
	}
	if in.InboxName == "" {
		in.InboxName = display
	}
	mail := libredesk.MailServer{Host: s.cfg.MailHost, Port: 993, TLS: "tls", SkipVerify: s.cfg.MailTLSSkipVerify, Username: email, Password: secret}
	smtp := mail
	smtp.Port = 465
	inboxID, err := client.CreateEmailInbox(r.Context(), libredesk.NewEmailInbox{Name: in.InboxName, From: display + " <" + email + ">", IMAP: mail, SMTP: smtp})
	if err != nil {
		return nil, helpdeskFailure("Helpdesk inbox creation failed", err)
	}
	link, err := s.st.CreateHelpdeskLink(r.Context(), store.HelpdeskLink{DomainID: d.ID, AccountID: a.ID, HelpdeskID: h.ID, InboxID: inboxID, AppPasswordID: id})
	if err != nil {
		ctx, cancel := cleanupContext(r.Context())
		defer cancel()
		_ = client.DeleteInbox(ctx, inboxID)
		return nil, err
	}
	committed = true
	return reply{201, linkJSON(link)}, nil
}

func (s *Server) revokeHelpdeskLink(ctx context.Context, client HelpdeskAPI, link store.HelpdeskLink) error {
	if err := client.DeleteInbox(ctx, link.InboxID); err != nil && !errors.Is(err, libredesk.ErrNotFound) {
		// Revoking the mail credential is still necessary if Libredesk is down.
		s.log.Warn("helpdesk inbox deletion failed", "link_id", link.ID)
	}
	if err := s.sw.DeleteAppPassword(ctx, link.AccountID, link.AppPasswordID); err != nil && !errors.Is(err, stalwart.ErrNotFound) {
		return integrationFailure("App password revocation failed", err)
	}
	return s.st.DeleteHelpdeskLink(ctx, link.ID)
}

func (s *Server) disconnectHelpdesk(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, a, err := s.authorizedMailbox(r)
	if err != nil {
		return nil, err
	}
	id, err := positiveID(r.PathValue("link"), "link")
	if err != nil {
		return nil, err
	}
	link, err := s.st.GetHelpdeskLink(r.Context(), id)
	if err != nil {
		return nil, err
	}
	if link.DomainID != d.ID || link.AccountID != a.ID {
		return nil, authz.ErrNotFound
	}
	h, err := s.st.GetHelpdesk(r.Context(), link.HelpdeskID)
	if err != nil {
		return nil, err
	}
	if h.TenantID != d.TenantID {
		return nil, authz.ErrNotFound
	}
	client, err := s.helpdeskClient(h)
	if err != nil {
		return nil, err
	}
	return reply{204, nil}, s.revokeHelpdeskLink(r.Context(), client, link)
}

func migrationJSON(j store.MigrationJob) map[string]any {
	return map[string]any{"id": j.ID, "domain_id": j.DomainID, "account_id": j.AccountID, "source_host": j.SourceHost, "source_port": j.SourcePort, "source_tls": j.SourceTLS, "source_user": j.SourceUser, "status": j.Status, "folders_total": j.FoldersTotal, "folders_done": j.FoldersDone, "messages_total": j.MessagesTotal, "messages_copied": j.MessagesCopied, "messages_skipped": j.MessagesSkipped, "error": j.Error, "created_by": j.CreatedBy, "created_at": j.CreatedAt, "started_at": j.StartedAt, "finished_at": j.FinishedAt}
}

func (s *Server) createMigration(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		AccountID      string `json:"account_id"`
		SourceHost     string `json:"source_host"`
		SourcePort     int    `json:"source_port"`
		SourceTLS      string `json:"source_tls"`
		SourceUser     string `json:"source_user"`
		SourcePassword string `json:"source_password"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if _, _, err := s.az.Mailbox(r.Context(), state(r).principal, d.ID, in.AccountID); err != nil {
		return nil, err
	}
	if err := s.requireMailHost(); err != nil {
		return nil, err
	}
	if in.SourceTLS == "" {
		in.SourceTLS = "tls"
	}
	if in.SourceTLS != "tls" && in.SourceTLS != "starttls" && in.SourceTLS != "none" {
		return nil, invalid("source_tls")
	}
	if _, err := hostname(in.SourceHost, "source_host"); err != nil && net.ParseIP(in.SourceHost) == nil {
		return nil, invalid("source_host")
	}
	if in.SourceHost != strings.TrimSpace(in.SourceHost) || in.SourcePort < 1 || in.SourcePort > 65535 || strings.TrimSpace(in.SourceUser) == "" || in.SourcePassword == "" {
		return nil, invalid("source endpoint")
	}
	enc, err := s.box.Seal([]byte(in.SourcePassword))
	if err != nil {
		return nil, err
	}
	userID := state(r).user.ID
	job, err := s.st.CreateMigrationJob(r.Context(), store.MigrationJob{DomainID: d.ID, AccountID: in.AccountID, SourceHost: in.SourceHost, SourcePort: in.SourcePort, SourceTLS: in.SourceTLS, SourceUser: in.SourceUser, SourceSecretEnc: enc, CreatedBy: &userID})
	return reply{201, migrationJSON(job)}, err
}

func (s *Server) migrations(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return nil, err
	}
	jobs, err := s.st.ListMigrationJobs(r.Context(), d.ID, 200)
	out := make([]any, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, migrationJSON(j))
	}
	return out, err
}

func (s *Server) cancelMigration(_ http.ResponseWriter, r *http.Request) (any, error) {
	d, err := s.authorizedDomain(r)
	if err != nil {
		return nil, err
	}
	id, err := positiveID(r.PathValue("mig"), "mig")
	if err != nil {
		return nil, err
	}
	job, err := s.st.GetMigrationJob(r.Context(), id)
	if err != nil {
		return nil, err
	}
	if job.DomainID != d.ID {
		return nil, authz.ErrNotFound
	}
	if _, _, err := s.az.Mailbox(r.Context(), state(r).principal, d.ID, job.AccountID); err != nil {
		return nil, err
	}
	if err := s.st.RequestCancelMigration(r.Context(), id); err != nil {
		return nil, err
	}
	job, err = s.st.GetMigrationJob(r.Context(), id)
	return migrationJSON(job), err
}
