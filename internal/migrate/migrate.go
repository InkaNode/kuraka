// Package migrate copies IMAP mailboxes while preserving message flags and
// internal dates. Run can be repeated to resume a partially completed migration.
package migrate

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

const batchSize = 50

// Endpoint identifies an IMAP account. TLS is "tls" (also the default),
// "starttls", or "none". SkipVerify is intended for self-signed lab servers.
type Endpoint struct {
	Host       string
	Port       int
	TLS        string
	SkipVerify bool
	Username   string
	Password   string
}

type folderSnapshot struct {
	folder   *imap.ListData
	messages []*imapclient.FetchMessageBuffer
}

// Run copies every selectable source folder into dst. report may be nil and is
// called synchronously, at most every 250ms except at each completed folder.
// The returned snapshot includes progress made before an error or cancellation.
func Run(ctx context.Context, src, dst Endpoint, report func(Progress)) (p Progress, err error) {
	defer func() {
		if ctx.Err() != nil {
			err = ctx.Err()
		} else if err != nil {
			err = redactError(err, src.Password, dst.Password)
		}
	}()
	if err := ctx.Err(); err != nil {
		return p, err
	}
	source, err := connect(ctx, src)
	if err != nil {
		return p, fmt.Errorf("migrate: connect source: %w", err)
	}
	defer disconnect(source, context.AfterFunc(ctx, func() { _ = source.Close() }))
	if err := source.Login(src.Username, src.Password).Wait(); err != nil {
		return p, fmt.Errorf("migrate: login source: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return p, err
	}
	target, err := connect(ctx, dst)
	if err != nil {
		return p, fmt.Errorf("migrate: connect target: %w", err)
	}
	defer disconnect(target, context.AfterFunc(ctx, func() { _ = target.Close() }))
	if err := target.Login(dst.Username, dst.Password).Wait(); err != nil {
		return p, fmt.Errorf("migrate: login target: %w", err)
	}
	folders, err := listFolders(source)
	if err != nil {
		return p, fmt.Errorf("migrate: list source folders: %w", err)
	}
	targets, err := listFolders(target)
	if err != nil {
		return p, fmt.Errorf("migrate: list target folders: %w", err)
	}
	delim := targetDelimiter(targets)
	if len(targets) == 0 {
		// LIST "" "" also discovers the delimiter of an empty namespace.
		root, err := target.List("", "", nil).Collect()
		if err != nil {
			return p, fmt.Errorf("migrate: discover target delimiter: %w", err)
		}
		delim = targetDelimiter(root)
	}
	var snapshots []folderSnapshot
	for _, folder := range folders {
		if !hasAttr(folder, imap.MailboxAttrNoSelect) {
			snapshots = append(snapshots, folderSnapshot{folder: folder})
		}
	}
	p.FoldersTotal = len(snapshots)
	// Gather metadata first so every callback has the total for the whole job.
	for i := range snapshots {
		if err := ctx.Err(); err != nil {
			return p, err
		}
		snapshot := &snapshots[i]
		selected, err := source.Select(snapshot.folder.Mailbox, &imap.SelectOptions{ReadOnly: true}).Wait()
		if err != nil {
			return p, fmt.Errorf("migrate: examine source %q: %w", snapshot.folder.Mailbox, err)
		}
		if selected.NumMessages != 0 {
			snapshot.messages, err = source.Fetch(allUIDs(), &imap.FetchOptions{
				Envelope: true, Flags: true, InternalDate: true, RFC822Size: true,
			}).Collect()
			if err != nil {
				return p, fmt.Errorf("migrate: fetch source metadata %q: %w", snapshot.folder.Mailbox, err)
			}
		}
		p.MessagesTotal += len(snapshot.messages)
	}
	r := &progressReporter{report: report, now: time.Now}
	known := make(map[string]bool)
	for _, folder := range targets {
		known[folder.Mailbox] = true
	}
	for _, snapshot := range snapshots {
		if err := ctx.Err(); err != nil {
			return p, err
		}
		p.Folder = snapshot.folder.Mailbox
		name := mapFolder(snapshot.folder, targets, delim)
		if err := ensureFolder(ctx, target, name, delim, known); err != nil {
			return p, fmt.Errorf("migrate: create target %q: %w", name, err)
		}
		// Remember inferred roles for subsequent source folders sharing that role.
		targets = append(targets, &imap.ListData{
			Mailbox: name, Delim: delim, Attrs: []imap.MailboxAttr{specialUse(snapshot.folder)},
		})
		if _, err := source.Select(p.Folder, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
			return p, fmt.Errorf("migrate: examine source %q: %w", p.Folder, err)
		}
		seen, err := targetKeys(ctx, target, name)
		if err != nil {
			return p, fmt.Errorf("migrate: read target %q: %w", name, err)
		}
		for start := 0; start < len(snapshot.messages); start += batchSize {
			end := min(start+batchSize, len(snapshot.messages))
			if err := copyBatch(ctx, source, target, name, snapshot.messages[start:end], seen, &p, r); err != nil {
				return p, fmt.Errorf("migrate: copy source %q to %q: %w", p.Folder, name, err)
			}
		}
		if err := ctx.Err(); err != nil {
			return p, err
		}
		p.FoldersDone++
		r.emit(p, true)
	}
	return p, nil
}

func connect(ctx context.Context, endpoint Endpoint) (*imapclient.Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if endpoint.Host == "" || endpoint.Port < 1 || endpoint.Port > 65535 {
		return nil, errors.New("invalid IMAP host or port")
	}
	dialer := &net.Dialer{Timeout: 20 * time.Second}
	if deadline, ok := ctx.Deadline(); ok {
		dialer.Deadline = deadline
	}
	options := &imapclient.Options{
		Dialer: dialer,
		TLSConfig: &tls.Config{
			ServerName: endpoint.Host, InsecureSkipVerify: endpoint.SkipVerify,
		},
	}
	address := net.JoinHostPort(endpoint.Host, strconv.Itoa(endpoint.Port))
	switch endpoint.TLS {
	case "", "tls":
		return imapclient.DialTLS(address, options)
	case "starttls":
		return imapclient.DialStartTLS(address, options)
	case "none":
		return imapclient.DialInsecure(address, options)
	default:
		return nil, errors.New("invalid TLS mode (want tls, starttls, or none)")
	}
}

func disconnect(client *imapclient.Client, stop func() bool) {
	defer stop()
	defer client.Close()
	_ = client.Logout().Wait()
}

func listFolders(client *imapclient.Client) ([]*imap.ListData, error) {
	caps := client.Caps()
	if caps.Has(imap.CapSpecialUse) || caps.Has(imap.CapListExtended) {
		folders, err := client.List("", "*", &imap.ListOptions{ReturnSpecialUse: true}).Collect()
		if err == nil {
			return folders, nil
		}
		// Some servers advertise LIST-EXTENDED without implementing SPECIAL-USE.
		var status *imap.Error
		if !errors.As(err, &status) || (status.Type != imap.StatusResponseTypeBad && status.Type != imap.StatusResponseTypeNo) {
			return nil, err
		}
	}
	return client.List("", "*", nil).Collect()
}

func ensureFolder(ctx context.Context, client *imapclient.Client, name string, delim rune, known map[string]bool) error {
	for _, path := range folderPaths(name, delim) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if known[path] || strings.EqualFold(path, "INBOX") {
			continue
		}
		if err := client.Create(path, nil).Wait(); err != nil {
			var status *imap.Error
			if !errors.As(err, &status) || status.Code != imap.ResponseCodeAlreadyExists {
				return err
			}
		}
		known[path] = true
	}
	return nil
}

func allUIDs() imap.UIDSet {
	return imap.UIDSet{{Start: 1, Stop: 0}}
}

func targetKeys(ctx context.Context, client *imapclient.Client, name string) (map[string]bool, error) {
	seen := make(map[string]bool)
	selected, err := client.Select(name, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil || selected.NumMessages == 0 {
		return seen, err
	}
	// Discover actual UIDs first: batching numeric UID ranges can require millions
	// of empty commands for a mailbox whose old messages have been expunged.
	uids, err := client.Fetch(allUIDs(), &imap.FetchOptions{UID: true}).Collect()
	if err != nil {
		return nil, err
	}
	for start := 0; start < len(uids); start += batchSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var set imap.UIDSet
		for _, message := range uids[start:min(start+batchSize, len(uids))] {
			set.AddNum(message.UID)
		}
		messages, err := client.Fetch(set, &imap.FetchOptions{
			Envelope: true, InternalDate: true, RFC822Size: true,
		}).Collect()
		if err != nil {
			return nil, err
		}
		for _, message := range messages {
			seen[messageKey(message)] = true
		}
	}
	return seen, nil
}

func messageKey(message *imapclient.FetchMessageBuffer) string {
	if message.Envelope != nil {
		if id := strings.TrimSpace(message.Envelope.MessageID); id != "" {
			return "id:" + id
		}
	}
	// IMAP INTERNALDATE has second precision. Normalize the timezone as well.
	return "date-size:" + message.InternalDate.UTC().Truncate(time.Second).Format(time.RFC3339) +
		":" + strconv.FormatInt(message.RFC822Size, 10)
}

func appendFlags(flags []imap.Flag) []imap.Flag {
	result := make([]imap.Flag, 0, len(flags))
	for _, flag := range flags {
		if !strings.EqualFold(string(flag), `\Recent`) {
			result = append(result, flag)
		}
	}
	return result
}

func copyBatch(ctx context.Context, source, target *imapclient.Client, name string, messages []*imapclient.FetchMessageBuffer, seen map[string]bool, p *Progress, r *progressReporter) error {
	pending := make(map[imap.UID]*imapclient.FetchMessageBuffer)
	var set imap.UIDSet
	for _, message := range messages {
		if err := ctx.Err(); err != nil {
			return err
		}
		if seen[messageKey(message)] {
			p.MessagesSkipped++
			r.emit(*p, false)
		} else {
			pending[message.UID] = message
			set.AddNum(message.UID)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	section := &imap.FetchItemBodySection{Peek: true}
	command := source.Fetch(set, &imap.FetchOptions{BodySection: []*imap.FetchItemBodySection{section}})
	defer command.Close()
	for data := command.Next(); data != nil; data = command.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Buffer one message at a time rather than retaining fifty bodies.
		body, err := data.Collect()
		if err != nil {
			return fmt.Errorf("fetch body: %w", err)
		}
		metadata, ok := pending[body.UID]
		if !ok {
			return fmt.Errorf("unexpected source UID %d", body.UID)
		}
		key := messageKey(metadata)
		if seen[key] {
			p.MessagesSkipped++
		} else {
			content := body.FindBodySection(section)
			if content == nil {
				return fmt.Errorf("missing body for source UID %d", body.UID)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := appendMessage(target, name, content, metadata.Flags, metadata.InternalDate); err != nil {
				return fmt.Errorf("append source UID %d: %w", body.UID, err)
			}
			seen[key] = true
			p.MessagesCopied++
		}
		delete(pending, body.UID)
		r.emit(*p, false)
	}
	if err := command.Close(); err != nil {
		return fmt.Errorf("fetch bodies: %w", err)
	}
	if len(pending) != 0 {
		return errors.New("source messages disappeared during migration; rerun to resume")
	}
	return ctx.Err()
}

func appendMessage(client *imapclient.Client, name string, body []byte, flags []imap.Flag, date time.Time) error {
	command := client.Append(name, int64(len(body)), &imap.AppendOptions{Flags: appendFlags(flags), Time: date})
	_, writeErr := command.Write(body)
	closeErr := command.Close()
	_, waitErr := command.Wait()
	return errors.Join(writeErr, closeErr, waitErr)
}

type redactedError struct {
	text  string
	cause error
}

func (e *redactedError) Error() string { return e.text }
func (e *redactedError) Unwrap() error { return e.cause }

func redactError(err error, passwords ...string) error {
	text := err.Error()
	for _, password := range passwords {
		if password != "" {
			text = strings.ReplaceAll(text, password, "[redacted]")
		}
	}
	return &redactedError{text: text, cause: err}
}
