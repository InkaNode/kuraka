package libredesk

import (
	"context"
	"errors"
	"net/http"
	"strconv"
)

// MetaPage is a Messenger or Instagram inbox on a Facebook Page. PageID is
// the Page for Messenger and the linked Instagram account for Instagram. The
// secrets are write-only, as in WhatsApp.
type MetaPage struct {
	Name        string `json:"name"`
	PageID      string `json:"page_id"`
	AccessToken string `json:"access_token,omitempty"`
	AppSecret   string `json:"app_secret,omitempty"`
}

func applyMetaPage(config map[string]any, p MetaPage, verifyToken string) {
	config["page_id"] = p.PageID
	config["access_token"], config["app_secret"] = p.AccessToken, p.AppSecret
	config["webhook_verify_token"] = verifyToken
}

// CreateMetaPage creates an enabled inbox of channel "messenger" or
// "instagram" and returns its id.
func (c *Client) CreateMetaPage(ctx context.Context, channel string, p MetaPage, verifyToken string) (int64, error) {
	config := map[string]any{}
	applyMetaPage(config, p, verifyToken)
	body := map[string]any{"name": p.Name, "channel": channel, "enabled": true, "csat_enabled": false, "config": config}
	var out Inbox
	if err := c.do(ctx, http.MethodPost, "/api/v1/inboxes", body, &out); err != nil {
		return 0, err
	}
	if out.ID == 0 {
		return 0, errors.New("libredesk: " + channel + " create returned no id")
	}
	return out.ID, nil
}

// GetMetaPage returns a Messenger or Instagram inbox without its secrets. A
// missing inbox yields ErrNotFound.
func (c *Client) GetMetaPage(ctx context.Context, id int64, channel string) (MetaPage, error) {
	inbox, config, err := c.channelInbox(ctx, id, channel)
	if err != nil {
		return MetaPage{}, err
	}
	name, _ := inbox["name"].(string)
	page, _ := config["page_id"].(string)
	return MetaPage{Name: name, PageID: page}, nil
}

// UpdateMetaPage writes p into a Messenger or Instagram inbox, keeping its
// other settings. Empty secrets keep the stored ones.
func (c *Client) UpdateMetaPage(ctx context.Context, id int64, channel string, p MetaPage, verifyToken string) error {
	inbox, config, err := c.channelInbox(ctx, id, channel)
	if err != nil {
		return err
	}
	inbox["name"] = p.Name
	applyMetaPage(config, p, verifyToken)
	return c.do(ctx, http.MethodPut, "/api/v1/inboxes/"+strconv.FormatInt(id, 10), inbox, nil)
}
