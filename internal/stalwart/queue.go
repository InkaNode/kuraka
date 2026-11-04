package stalwart

import (
	"context"
	"time"
)

const objQueue = "x:QueuedMessage"

type QueuedMessage struct {
	ID         string                     `json:"id"`
	CreatedAt  string                     `json:"createdAt"`
	NextRetry  string                     `json:"nextRetry"`
	ReturnPath string                     `json:"returnPath"`
	Size       uint64                     `json:"size"`
	Recipients map[string]QueuedRecipient `json:"recipients"`
}

type QueuedRecipient struct {
	RetryCount int `json:"retryCount"`
	Status     struct {
		Type         string `json:"@type"`
		ErrorMessage string `json:"errorMessage"`
	} `json:"status"`
}

func (c *Client) ListQueue(ctx context.Context) ([]QueuedMessage, error) {
	// Pagination is the upgrade when the first 500 messages are no longer enough.
	resps, err := c.call(ctx, []invocation{
		{objQueue + "/query", map[string]any{"sort": []any{map[string]any{"property": "due", "isAscending": true}}, "limit": 500}, "q"},
		{objQueue + "/get", map[string]any{"#ids": map[string]any{"resultOf": "q", "name": objQueue + "/query", "path": "/ids"}}, "g"},
	})
	if err != nil {
		return nil, err
	}
	var g getResult[QueuedMessage]
	if err := result(resps, "g", &g); err != nil {
		return nil, err
	}
	return g.List, nil
}

func (c *Client) GetQueued(ctx context.Context, id string) (QueuedMessage, error) {
	return getOne[QueuedMessage](ctx, c, objQueue, id)
}

func (c *Client) RetryQueued(ctx context.Context, id string) error {
	return c.update(ctx, objQueue, id, map[string]any{"nextRetry": time.Now().UTC().Format(time.RFC3339)})
}

func (c *Client) CancelQueued(ctx context.Context, id string) error {
	return c.destroy(ctx, objQueue, id)
}
