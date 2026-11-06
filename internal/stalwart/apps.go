package stalwart

import "context"

// UpdateApps makes Stalwart download its application bundles (the web
// interface) again from their resource URLs.
func (c *Client) UpdateApps(ctx context.Context) error {
	_, err := c.create(ctx, "x:Action", map[string]any{"@type": "UpdateApps"})
	return err
}

// SetWebUIURL points the application served at /admin (the web interface)
// to url. It does nothing when it already does.
func (c *Client) SetWebUIURL(ctx context.Context, url string) error {
	resps, err := c.call(ctx, []invocation{{"x:Application/get", map[string]any{}, "g"}})
	if err != nil {
		return err
	}
	var r getResult[struct {
		ID          string          `json:"id"`
		URLPrefix   map[string]bool `json:"urlPrefix"`
		ResourceURL string          `json:"resourceUrl"`
	}]
	if err := result(resps, "g", &r); err != nil {
		return err
	}
	for _, app := range r.List {
		if app.URLPrefix["/admin"] && app.ResourceURL != url {
			return c.update(ctx, "x:Application", app.ID, map[string]any{"resourceUrl": url})
		}
	}
	return nil
}
