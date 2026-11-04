package stalwart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

const queueGet = `{"list":[{"id":"q1","createdAt":"2026-11-03T00:00:00Z","nextRetry":"2026-11-03T00:01:00Z","returnPath":"sender@example.test","size":123,"recipients":{"to@example.test":{"retryCount":2,"status":{"@type":"TemporaryFailure","errorMessage":"try later"}}}}],"notFound":[]}`

func TestListQueue(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertNames(t, calls, "x:QueuedMessage/query", "x:QueuedMessage/get")
		assertJSON(t, "query args", calls[0].Args, `{"sort":[{"property":"due","isAscending":true}],"limit":500}`)
		assertJSON(t, "get args", calls[1].Args, fmt.Sprintf(idsRef, objQueue))
		return []any{resp(objQueue+"/query", "q", `{"ids":["q1"]}`), resp(objQueue+"/get", "g", queueGet)}
	})
	messages, err := c.ListQueue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].ID != "q1" || messages[0].Size != 123 {
		t.Fatalf("messages = %+v", messages)
	}
	recipient := messages[0].Recipients["to@example.test"]
	if recipient.RetryCount != 2 || recipient.Status.Type != "TemporaryFailure" || recipient.Status.ErrorMessage != "try later" {
		t.Fatalf("recipient = %+v", recipient)
	}
}

func TestGetQueued(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertNames(t, calls, objQueue+"/get")
		if string(calls[0].Args) == `{"ids":["missing"]}` {
			return []any{resp(objQueue+"/get", "g", `{"list":[],"notFound":["missing"]}`)}
		}
		assertJSON(t, "get args", calls[0].Args, `{"ids":["q1"]}`)
		return []any{resp(objQueue+"/get", "g", queueGet)}
	})
	message, err := c.GetQueued(context.Background(), "q1")
	if err != nil || message.ID != "q1" {
		t.Fatalf("GetQueued = %+v, %v", message, err)
	}
	if _, err := c.GetQueued(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing error = %v", err)
	}
}

func TestRetryQueued(t *testing.T) {
	before := time.Now().UTC().Truncate(time.Second)
	c := newFake(t, func(calls []fakeCall) []any {
		assertNames(t, calls, objQueue+"/set")
		var args struct {
			Update map[string]map[string]string `json:"update"`
		}
		if err := json.Unmarshal(calls[0].Args, &args); err != nil {
			t.Fatal(err)
		}
		date := args.Update["q1"]["nextRetry"]
		at, err := time.Parse(time.RFC3339, date)
		if err != nil || at.Before(before) || at.After(time.Now()) || at.Location() != time.UTC {
			t.Errorf("nextRetry = %q, %v", date, err)
		}
		assertJSON(t, "retry args", calls[0].Args, fmt.Sprintf(`{"update":{"q1":{"nextRetry":%q}}}`, date))
		return []any{resp(objQueue+"/set", "s", `{"updated":{"q1":null}}`)}
	})
	if err := c.RetryQueued(context.Background(), "q1"); err != nil {
		t.Fatal(err)
	}
}

func TestCancelQueued(t *testing.T) {
	c := newFake(t, func(calls []fakeCall) []any {
		assertNames(t, calls, objQueue+"/set")
		assertJSON(t, "cancel args", calls[0].Args, `{"destroy":["q1"]}`)
		return []any{resp(objQueue+"/set", "s", `{"destroyed":["q1"]}`)}
	})
	if err := c.CancelQueued(context.Background(), "q1"); err != nil {
		t.Fatal(err)
	}
	missing := newFake(t, func(calls []fakeCall) []any {
		return []any{resp(objQueue+"/set", "s", `{"notUpdated":{"q1":{"type":"notFound"}},"notDestroyed":{"q1":{"type":"notFound"}}}`)}
	})
	if err := missing.RetryQueued(context.Background(), "q1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("retry error = %v", err)
	}
	if err := missing.CancelQueued(context.Background(), "q1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cancel error = %v", err)
	}
}
