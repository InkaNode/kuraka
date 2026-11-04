package jobs

import (
	"strings"
	"testing"
	"time"

	"github.com/InkaNode/kuraka/internal/store"
)

func TestAlertMessage(t *testing.T) {
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	rule := store.AlertRule{ID: 3, Metric: "queue_size", Threshold: 10}
	msg, err := alertMessage("alertas@example.com", []string{"a@example.com", "b@example.com"}, "abc@mail.example.com", rule, 12.5, "firing", at)
	if err != nil {
		t.Fatal(err)
	}
	s := string(msg)
	for _, want := range []string{
		"From: alertas@example.com\r\n",
		"To: a@example.com, b@example.com\r\n",
		"Subject: [Kuraka] firing: queue_size\r\n",
		"Date: Wed, 04 Nov 2026 12:00:00 +0000\r\n",
		"Message-ID: <abc@mail.example.com>\r\n",
		"MIME-Version: 1.0\r\n",
		"Content-Type: text/plain; charset=utf-8\r\n",
		"\r\n\r\nMetric: queue_size\r\nState: firing\r\nValue: 12.5\r\nThreshold: 10\r\nTime: 2026-11-04T12:00:00Z\r\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("message missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(strings.ReplaceAll(s, "\r\n", ""), "\n") {
		t.Error("message has bare LF")
	}
	rule.Metric = "queue_size\r\nBcc: evil@example.com"
	if _, err := alertMessage("alertas@example.com", []string{"a@example.com"}, "abc@mail.example.com", rule, 1, "firing", at); err == nil {
		t.Error("header injection accepted")
	}
}
