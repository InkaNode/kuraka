package migrate

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// TestRunIMAP requires two dedicated test accounts. Run migrates all their
// source folders. Endpoints use user:pass@host:port (URL-escape credentials).
// Port 993 uses TLS, 143 uses STARTTLS, and other ports use plaintext for local
// test servers. Optional KURAKA_IT_IMAP_{SRC,DST}_TLS overrides that choice;
// KURAKA_IT_IMAP_SKIP_VERIFY=true permits self-signed test certificates.
func TestRunIMAP(t *testing.T) {
	if os.Getenv("KURAKA_IT_IMAP_SRC") == "" || os.Getenv("KURAKA_IT_IMAP_DST") == "" {
		t.Skip("set KURAKA_IT_IMAP_SRC and KURAKA_IT_IMAP_DST to dedicated IMAP test accounts")
	}
	src := integrationEndpoint(t, "SRC")
	dst := integrationEndpoint(t, "DST")
	if src.Host == dst.Host && src.Port == dst.Port && src.Username == dst.Username {
		t.Fatal("integration test needs distinct source and target accounts")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	source := integrationClient(t, ctx, src)
	target := integrationClient(t, ctx, dst)
	var token [12]byte
	if _, err := rand.Read(token[:]); err != nil {
		t.Fatal(err)
	}
	folder := "KurakaMigrationTest-" + hex.EncodeToString(token[:])
	if err := source.Create(folder, nil).Wait(); err != nil {
		t.Fatalf("create fixture: %v", redactError(err, src.Password, dst.Password))
	}
	// Only the unique fixture folder is removed; existing account mail survives.
	t.Cleanup(func() {
		for _, client := range []*imapclient.Client{source, target} {
			_, _ = client.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait()
			if err := client.Delete(folder).Wait(); err != nil {
				t.Errorf("remove fixture: %v", redactError(err, src.Password, dst.Password))
			}
		}
	})
	date := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	wantBodies := make(map[string]bool)
	for i := 0; i < 53; i++ {
		id := fmt.Sprintf("Message-ID: <kuraka-%s-%d@example.org>\r\n", folder, i)
		if i >= 51 {
			id = "" // Exercise fallback deduplication across an entire second run.
		}
		body := []byte(fmt.Sprintf("From: sender@example.org\r\nTo: recipient@example.org\r\n%sSubject: migration %d\r\n\r\nBody %d\r\n", id, i, i))
		flags := []imap.Flag{imap.FlagFlagged}
		if i%2 == 0 {
			flags = append(flags, imap.FlagSeen)
		}
		if err := appendMessage(source, folder, body, flags, date.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("append fixture: %v", redactError(err, src.Password, dst.Password))
		}
		wantBodies[string(body)] = true
	}
	var last Progress
	first, err := Run(ctx, src, dst, func(p Progress) { last = p })
	if err != nil {
		t.Fatalf("first migration: %v", err)
	}
	if first.MessagesCopied < 53 || first.MessagesTotal != first.MessagesCopied+first.MessagesSkipped || first.FoldersDone != first.FoldersTotal || last != first {
		t.Fatalf("inconsistent first progress: %+v, callback %+v", first, last)
	}
	second, err := Run(ctx, src, dst, nil)
	if err != nil {
		t.Fatalf("second migration: %v", err)
	}
	if second.MessagesCopied != 0 || second.MessagesSkipped != second.MessagesTotal {
		t.Fatalf("second migration was not idempotent: %+v", second)
	}
	section := &imap.FetchItemBodySection{Peek: true}
	options := &imap.FetchOptions{
		Envelope: true, Flags: true, InternalDate: true, RFC822Size: true,
		BodySection: []*imap.FetchItemBodySection{section},
	}
	read := func(client *imapclient.Client) []*imapclient.FetchMessageBuffer {
		t.Helper()
		if _, err := client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
			t.Fatalf("select fixture: %v", redactError(err, src.Password, dst.Password))
		}
		messages, err := client.Fetch(allUIDs(), options).Collect()
		if err != nil {
			t.Fatalf("fetch fixture: %v", redactError(err, src.Password, dst.Password))
		}
		return messages
	}
	originals := read(source)
	copies := read(target)
	if len(originals) != 53 || len(copies) != 53 {
		t.Fatalf("fixture counts: source %d, target %d", len(originals), len(copies))
	}
	byBody := make(map[string]*imapclient.FetchMessageBuffer)
	for _, message := range copies {
		body := string(message.FindBodySection(section))
		if !wantBodies[body] || byBody[body] != nil {
			t.Fatal("target contains an unexpected or duplicate body")
		}
		byBody[body] = message
	}
	for _, original := range originals {
		body := original.FindBodySection(section)
		copy := byBody[string(body)]
		if copy == nil || !bytes.Equal(body, copy.FindBodySection(section)) || !original.InternalDate.Equal(copy.InternalDate) {
			t.Fatal("body or internal date changed")
		}
		for _, flag := range []imap.Flag{imap.FlagSeen, imap.FlagFlagged} {
			if hasFlag(original.Flags, flag) != hasFlag(copy.Flags, flag) {
				t.Fatalf("flag %q changed", flag)
			}
		}
		i, err := strconv.Atoi(strings.TrimPrefix(original.Envelope.Subject, "migration "))
		if err != nil {
			t.Fatal("unexpected fixture subject")
		}
		if hasFlag(original.Flags, imap.FlagSeen) != (i%2 == 0) {
			t.Fatal("migration changed source Seen flags")
		}
	}
}

func integrationEndpoint(t *testing.T, suffix string) Endpoint {
	t.Helper()
	u, err := url.Parse("imap://" + os.Getenv("KURAKA_IT_IMAP_"+suffix))
	if err != nil || u.User == nil || u.Hostname() == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		t.Fatal("invalid KURAKA_IT_IMAP_" + suffix + ": want user:pass@host:port")
	}
	_, portText, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal("invalid host:port in KURAKA_IT_IMAP_" + suffix)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		t.Fatal("invalid port in KURAKA_IT_IMAP_" + suffix)
	}
	password, ok := u.User.Password()
	if !ok {
		t.Fatal("missing password in KURAKA_IT_IMAP_" + suffix)
	}
	mode := "none"
	if port == 993 {
		mode = "tls"
	} else if port == 143 {
		mode = "starttls"
	}
	if override := os.Getenv("KURAKA_IT_IMAP_" + suffix + "_TLS"); override != "" {
		mode = override
	}
	return Endpoint{
		Host: u.Hostname(), Port: port, TLS: mode, Username: u.User.Username(), Password: password,
		SkipVerify: strings.EqualFold(os.Getenv("KURAKA_IT_IMAP_SKIP_VERIFY"), "true"),
	}
}

func integrationClient(t *testing.T, ctx context.Context, endpoint Endpoint) *imapclient.Client {
	t.Helper()
	client, err := connect(ctx, endpoint)
	if err != nil {
		t.Fatalf("connect fixture account: %v", redactError(err, endpoint.Password))
	}
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	t.Cleanup(func() { disconnect(client, stop) })
	if err := client.Login(endpoint.Username, endpoint.Password).Wait(); err != nil {
		t.Fatalf("login fixture account: %v", redactError(err, endpoint.Password))
	}
	return client
}

func hasFlag(flags []imap.Flag, want imap.Flag) bool {
	for _, flag := range flags {
		if strings.EqualFold(string(flag), string(want)) {
			return true
		}
	}
	return false
}
