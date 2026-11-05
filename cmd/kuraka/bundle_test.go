package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/InkaNode/kuraka/internal/provision"
)

func TestZipToTar(t *testing.T) {
	bundle, err := provision.Bundle("test", "desk.example.com", provision.Options{})
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(bundle.Zip), int64(len(bundle.Zip)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := zipToTar(&out, bundle.Zip); err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(&out)
	for _, file := range z.File {
		h, err := tr.Next()
		if err != nil {
			t.Fatal(err)
		}
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		want, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(tr)
		mode := int64(0600)
		if file.Name == "install.sh" {
			mode = 0700
		}
		if err != nil || h.Name != file.Name || h.Mode != mode || !bytes.Equal(got, want) {
			t.Fatalf("tar entry %s: %+v %v", file.Name, h, err)
		}
	}
	if _, err := tr.Next(); err != io.EOF {
		t.Fatalf("tar end: %v", err)
	}
	if err := zipToTar(io.Discard, []byte("invalid")); err == nil {
		t.Fatal("accepted invalid zip")
	}
	if err := zipToTar(failingWriter{}, bundle.Zip); err == nil {
		t.Fatal("ignored write failure")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestBundleCommand(t *testing.T) {
	args, stdout := os.Args, os.Stdout
	defer func() { os.Args, os.Stdout = args, stdout }()
	f, err := os.CreateTemp(t.TempDir(), "bundle-*")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	os.Stdout = f
	password := "Aa9!" + strings.Repeat("a", 48)
	t.Setenv("KURAKA_BUNDLE_SYSTEM_PASSWORD", password)
	os.Args = []string{"kuraka", "bundle", "test", "desk.example.com"}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(f)
	found := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Name == ".env" {
			data, err := io.ReadAll(tr)
			if err != nil || !bytes.Contains(data, []byte("LIBREDESK_SYSTEM_USER_PASSWORD="+password+"\n")) {
				t.Fatal("command password mismatch")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("missing .env")
	}
	for _, tail := range [][]string{
		{}, {"test"}, {"test", "desk.example.com", "extra"},
		{"../test", "desk.example.com"}, {"Test", "desk.example.com"},
		{"-test", "desk.example.com"}, {"test-", "desk.example.com"}, {"test--one", "desk.example.com"},
		{strings.Repeat("a", 49), "desk.example.com"}, {"test", "localhost"},
		{"test", "desk.example.com\n"}, {"test", "desk;echo.example.com"},
	} {
		os.Args = append([]string{"kuraka", "bundle"}, tail...)
		if err := run(); err == nil {
			t.Fatalf("accepted arguments: %q", tail)
		}
	}
	os.Args = []string{"kuraka", "bundle", "test", "desk.example.com"}
	t.Setenv("KURAKA_BUNDLE_SYSTEM_PASSWORD", "weak")
	if err := run(); err == nil || err.Error() != "invalid bundle options" {
		t.Fatalf("invalid password: %v", err)
	}
}
