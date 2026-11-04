package stalwart

import "context"

// UpdateApps makes Stalwart download its application bundles (the web
// interface) again from their resource URLs.
func (c *Client) UpdateApps(ctx context.Context) error {
	_, err := c.create(ctx, "x:Action", map[string]any{"@type": "UpdateApps"})
	return err
}
