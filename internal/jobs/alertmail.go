package jobs

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/InkaNode/kuraka/internal/config"
	"github.com/InkaNode/kuraka/internal/stalwart"
	"github.com/InkaNode/kuraka/internal/store"
)

// sendAlertMail emails an alert transition to every enabled superadmin from
// cfg.AlertEmailFrom, authenticating with a temporary app password.
func sendAlertMail(parent context.Context, st *store.Store, sw *stalwart.Client, cfg config.Config, log *slog.Logger, rule store.AlertRule, value float64, state string, at int64) {
	ctx, cancel := context.WithTimeout(parent, 25*time.Second)
	defer cancel()
	users, err := st.ListUsers(ctx, nil)
	if err != nil {
		log.Error("alert mail recipients lookup failed", "rule_id", rule.ID, "error", err)
		return
	}
	var to []string
	for _, user := range users {
		if user.Role == "superadmin" && !user.Disabled {
			to = append(to, user.Email)
		}
	}
	if len(to) == 0 {
		log.Warn("alert mail skipped: no enabled superadmins", "rule_id", rule.ID)
		return
	}
	from := cfg.AlertEmailFrom
	id := make([]byte, 16)
	rand.Read(id)
	msg, err := alertMessage(from, to, hex.EncodeToString(id)+"@"+cfg.MailHost, rule, value, state, time.Unix(at, 0))
	if err != nil {
		log.Error("alert mail build failed", "rule_id", rule.ID, "error", err)
		return
	}
	domain, err := st.GetDomainByName(ctx, from[strings.LastIndex(from, "@")+1:])
	if err != nil {
		log.Error("alert mail sender domain lookup failed", "rule_id", rule.ID, "error", err)
		return
	}
	accounts, err := sw.ListAccounts(ctx, domain.StalwartID)
	if err != nil {
		log.Error("alert mail sender lookup failed", "rule_id", rule.ID, "error", err)
		return
	}
	var accountID, login string
	for _, account := range accounts {
		email := account.Email
		if email == "" {
			email = account.Name + "@" + domain.Name
		}
		if strings.EqualFold(email, from) {
			accountID, login = account.ID, email
			break
		}
	}
	if accountID == "" {
		log.Error("alert mail sender mailbox not found", "rule_id", rule.ID)
		return
	}
	appID, secret, err := sw.CreateAppPassword(ctx, accountID, fmt.Sprintf("alert #%d", rule.ID))
	if err != nil {
		log.Error("alert mail app password creation failed", "rule_id", rule.ID, "error", err)
		return
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
		defer stop()
		if err := sw.DeleteAppPassword(cleanup, accountID, appID); err != nil && !errors.Is(err, stalwart.ErrNotFound) {
			log.Error("alert mail app password revocation failed", "rule_id", rule.ID, "error", err)
		}
	}()
	if err := sendMail(ctx, cfg, login, secret, from, to, msg); err != nil {
		log.Error("alert mail send failed", "rule_id", rule.ID, "error", err)
	}
}

// sendMail submits msg over implicit TLS on port 465.
func sendMail(ctx context.Context, cfg config.Config, login, secret, from string, to []string, msg []byte) error {
	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: &tls.Config{ServerName: cfg.MailHost, InsecureSkipVerify: cfg.MailTLSSkipVerify}}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(cfg.MailHost, "465"))
	if err != nil {
		return err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	c, err := smtp.NewClient(conn, cfg.MailHost)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Auth(smtp.PlainAuth("", login, secret, cfg.MailHost)); err != nil {
		return err
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	// One refused superadmin address must not silence the alert for the rest.
	var rcptErr error
	accepted := 0
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			rcptErr = err
			continue
		}
		accepted++
	}
	if accepted == 0 {
		return rcptErr
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// alertMessage builds a plain-text message with CRLF line endings. Header
// values containing CR or LF are rejected.
func alertMessage(from string, to []string, msgID string, rule store.AlertRule, value float64, state string, at time.Time) ([]byte, error) {
	at = at.UTC()
	headers := [][2]string{
		{"From", from},
		{"To", strings.Join(to, ", ")},
		{"Subject", "[Kuraka] " + state + ": " + rule.Metric},
		{"Date", at.Format(time.RFC1123Z)},
		{"Message-ID", "<" + msgID + ">"},
		{"MIME-Version", "1.0"},
		{"Content-Type", "text/plain; charset=utf-8"},
	}
	var b strings.Builder
	for _, h := range headers {
		if strings.ContainsAny(h[1], "\r\n") {
			return nil, fmt.Errorf("alert mail header %s contains a line break", h[0])
		}
		b.WriteString(h[0] + ": " + h[1] + "\r\n")
	}
	b.WriteString("\r\n")
	b.WriteString("Metric: " + rule.Metric + "\r\n")
	b.WriteString("State: " + state + "\r\n")
	b.WriteString("Value: " + strconv.FormatFloat(value, 'g', -1, 64) + "\r\n")
	b.WriteString("Threshold: " + strconv.FormatFloat(rule.Threshold, 'g', -1, 64) + "\r\n")
	b.WriteString("Time: " + at.Format(time.RFC3339) + "\r\n")
	return []byte(b.String()), nil
}
