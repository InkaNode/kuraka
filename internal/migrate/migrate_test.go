package migrate

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func TestSpecialUse(t *testing.T) {
	tests := []struct {
		names []string
		attr  imap.MailboxAttr
	}{
		{[]string{"Sent", "Sent Items", "Enviados"}, imap.MailboxAttrSent},
		{[]string{"Drafts", "Borradores"}, imap.MailboxAttrDrafts},
		{[]string{"Trash", "Deleted Items", "Papelera"}, imap.MailboxAttrTrash},
		{[]string{"Junk", "Spam"}, imap.MailboxAttrJunk},
		{[]string{"Archive"}, imap.MailboxAttrArchive},
		{[]string{"Projects", "Sentimental", "INBOX"}, ""},
	}
	for _, test := range tests {
		for _, name := range test.names {
			t.Run(name, func(t *testing.T) {
				folder := &imap.ListData{Mailbox: strings.ToUpper(name), Delim: '/'}
				if got := specialUse(folder); got != test.attr {
					t.Fatalf("specialUse(%q) = %q, want %q", name, got, test.attr)
				}
			})
		}
	}
	if got := specialUse(&imap.ListData{Mailbox: "INBOX.Enviados", Delim: '.'}); got != imap.MailboxAttrSent {
		t.Fatalf("nested localized folder: %q", got)
	}
	if got := specialUse(&imap.ListData{Mailbox: "Trash", Attrs: []imap.MailboxAttr{imap.MailboxAttrSent}}); got != imap.MailboxAttrSent {
		t.Fatalf("explicit role must override heuristic: %q", got)
	}
}

func TestMapFolder(t *testing.T) {
	targets := []*imap.ListData{
		{Mailbox: "INBOX", Delim: '/'},
		{Mailbox: "Enviados", Delim: '/'},
		{Mailbox: "Not selectable", Attrs: []imap.MailboxAttr{imap.MailboxAttrSent, imap.MailboxAttrNoSelect}},
		{Mailbox: "Mail/Sent mail", Attrs: []imap.MailboxAttr{imap.MailboxAttrSent}},
		{Mailbox: "Papelera", Attrs: []imap.MailboxAttr{imap.MailboxAttrTrash}},
	}
	tests := []struct {
		name   string
		source imap.ListData
		want   string
	}{
		{"inbox", imap.ListData{Mailbox: "inbox"}, "INBOX"},
		{"explicit sent", imap.ListData{Mailbox: "Outgoing", Attrs: []imap.MailboxAttr{imap.MailboxAttrSent}}, "Mail/Sent mail"},
		{"heuristic sent", imap.ListData{Mailbox: "INBOX.Sent Items", Delim: '.'}, "Mail/Sent mail"},
		{"trash", imap.ListData{Mailbox: "Deleted Items"}, "Papelera"},
		{"ordinary", imap.ListData{Mailbox: "Projects.Client.2026", Delim: '.'}, "Projects/Client/2026"},
		{"missing special", imap.ListData{Mailbox: "INBOX.Borradores", Delim: '.'}, "INBOX/Borradores"},
		{"flat source", imap.ListData{Mailbox: "Projects.Client", Delim: 0}, "Projects.Client"},
		{"unicode", imap.ListData{Mailbox: "Proyectos·Año", Delim: '·'}, "Proyectos/Año"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := mapFolder(&test.source, targets, '/'); got != test.want {
				t.Fatalf("mapFolder = %q, want %q", got, test.want)
			}
		})
	}
	if got := mapFolder(&imap.ListData{Mailbox: "Sent"}, targets[:2], '/'); got != "Enviados" {
		t.Fatalf("target name heuristic = %q", got)
	}
	if got := convertDelimiter("a.b", '.', 0); got != "a.b" {
		t.Fatalf("flat target name = %q", got)
	}
	if got := targetDelimiter(targets); got != '/' {
		t.Fatalf("target delimiter = %q", got)
	}
	if got := targetDelimiter(nil); got != 0 {
		t.Fatalf("empty target delimiter = %q", got)
	}
	shared := []*imap.ListData{{Mailbox: "Shared", Attrs: []imap.MailboxAttr{imap.MailboxAttrSent, imap.MailboxAttrArchive}}}
	if got := mapFolder(&imap.ListData{Mailbox: "Archive"}, shared, '/'); got != "Shared" {
		t.Fatalf("target with multiple roles = %q", got)
	}
}

func TestFolderPaths(t *testing.T) {
	for _, test := range []struct {
		name  string
		delim rune
		want  []string
	}{
		{"Projects/Client/2026", '/', []string{"Projects", "Projects/Client", "Projects/Client/2026"}},
		{"A.B", 0, []string{"A.B"}},
		{"INBOX", '/', []string{"INBOX"}},
		{"Proyectos·Año·Enero", '·', []string{"Proyectos", "Proyectos·Año", "Proyectos·Año·Enero"}},
	} {
		if got := folderPaths(test.name, test.delim); !reflect.DeepEqual(got, test.want) {
			t.Errorf("folderPaths(%q) = %v, want %v", test.name, got, test.want)
		}
	}
	if !hasAttr(&imap.ListData{Attrs: []imap.MailboxAttr{`\NOSELECT`}}, imap.MailboxAttrNoSelect) {
		t.Fatal("Noselect comparison must be case insensitive")
	}
}

func TestMessageKey(t *testing.T) {
	date := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	a := &imapclient.FetchMessageBuffer{Envelope: &imap.Envelope{MessageID: " mail@example.org "}, InternalDate: date, RFC822Size: 42}
	b := &imapclient.FetchMessageBuffer{Envelope: &imap.Envelope{MessageID: "mail@example.org"}, InternalDate: date.Add(time.Hour), RFC822Size: 999}
	if messageKey(a) != messageKey(b) {
		t.Fatal("same Message-ID must deduplicate regardless of date and size")
	}
	b.Envelope.MessageID = "MAIL@example.org"
	if messageKey(a) == messageKey(b) {
		t.Fatal("Message-ID local part must remain case sensitive")
	}
	a.Envelope = nil
	b.Envelope = &imap.Envelope{}
	if messageKey(a) != "" || messageKey(b) != "" {
		t.Fatal("missing IDs must defer to the content key")
	}
	if contentKey([]byte("x")) != contentKey([]byte("x")) || contentKey([]byte("x")) == contentKey([]byte("y")) {
		t.Fatal("content keys must match exactly equal bodies only")
	}
	if contentKey([]byte("a\nb")) != contentKey([]byte("a\r\nb")) {
		t.Fatal("content keys must ignore CRLF normalization")
	}
	b.Envelope.MessageID = contentKey([]byte("x"))
	if messageKey(b) == contentKey([]byte("x")) {
		t.Fatal("ID and content keys must have separate namespaces")
	}
}

func TestProgressThrottling(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var reports []Progress
	r := progressReporter{
		now:    func() time.Time { return now },
		report: func(p Progress) { reports = append(reports, p) },
	}
	r.emit(Progress{MessagesCopied: 1}, false)
	now = now.Add(249 * time.Millisecond)
	r.emit(Progress{MessagesCopied: 2}, false)
	if len(reports) != 1 {
		t.Fatalf("reported within throttle window: %v", reports)
	}
	now = now.Add(time.Millisecond)
	r.emit(Progress{MessagesCopied: 3}, false)
	r.emit(Progress{MessagesCopied: 4, FoldersDone: 1}, true)
	r.emit(Progress{MessagesCopied: 5}, false)
	if len(reports) != 3 || reports[1].MessagesCopied != 3 || reports[2].FoldersDone != 1 {
		t.Fatalf("expected boundary and forced folder-end snapshots: %v", reports)
	}
	r.emit(Progress{FoldersDone: 2}, true)
	if len(reports) != 4 {
		t.Fatal("empty folders must also report immediately at folder end")
	}
	(&progressReporter{}).emit(Progress{}, true) // nil report is allowed
}

func TestAppendFlags(t *testing.T) {
	flags := []imap.Flag{imap.FlagSeen, `\Recent`, imap.FlagAnswered, "custom", `\RECENT`, imap.FlagDeleted}
	want := []imap.Flag{imap.FlagSeen, imap.FlagAnswered, "custom", imap.FlagDeleted}
	if got := appendFlags(flags); !reflect.DeepEqual(got, want) {
		t.Fatalf("appendFlags = %v, want %v", got, want)
	}
	if flags[1] != `\Recent` {
		t.Fatal("appendFlags mutated source metadata")
	}
}

func TestCancelledRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p, err := Run(ctx, Endpoint{}, Endpoint{}, func(Progress) { t.Fatal("unexpected callback") })
	if err != context.Canceled || p != (Progress{}) {
		t.Fatalf("Run = %+v, %v", p, err)
	}
}

func TestEndpointValidation(t *testing.T) {
	for _, endpoint := range []Endpoint{
		{}, {Host: "localhost", Port: -1}, {Host: "localhost", Port: 65536},
		{Host: "localhost", Port: 993, TLS: "invalid"},
	} {
		if _, err := connect(context.Background(), endpoint); err == nil {
			t.Fatal("accepted invalid endpoint")
		}
	}
}

func TestRedactedError(t *testing.T) {
	cause := &imap.Error{Type: imap.StatusResponseTypeNo, Text: "rejected source-password and target-password"}
	err := redactError(fmt.Errorf("login: %w", cause), "source-password", "target-password", "")
	if strings.Contains(err.Error(), "source-password") || strings.Contains(err.Error(), "target-password") {
		t.Fatalf("password leaked: %v", err)
	}
	var status *imap.Error
	if !errors.As(err, &status) || !errors.Is(err, cause) {
		t.Fatal("redaction must preserve the wrapped error")
	}
}
