package libredesk

import (
	"context"
	"errors"
	"net/http"
	"strconv"
)

// WhatsApp is a WhatsApp Cloud API inbox. The access token and app secret are
// write-only: GetWhatsApp leaves them empty, and UpdateWhatsApp keeps the
// stored ones when they are empty, as Libredesk does.
type WhatsApp struct {
	Name          string `json:"name"`
	PhoneNumberID string `json:"phone_number_id"`
	WABAID        string `json:"waba_id"`
	AccessToken   string `json:"access_token,omitempty"`
	AppSecret     string `json:"app_secret,omitempty"`
}

func applyWhatsApp(config map[string]any, w WhatsApp, verifyToken string) {
	config["phone_number_id"], config["waba_id"] = w.PhoneNumberID, w.WABAID
	config["access_token"], config["app_secret"] = w.AccessToken, w.AppSecret
	config["webhook_verify_token"] = verifyToken
}

// CreateWhatsApp creates an enabled WhatsApp inbox and returns its id.
// Libredesk checks the credentials with Meta first.
func (c *Client) CreateWhatsApp(ctx context.Context, w WhatsApp, verifyToken string) (int64, error) {
	config := map[string]any{}
	applyWhatsApp(config, w, verifyToken)
	body := map[string]any{"name": w.Name, "channel": "whatsapp", "enabled": true, "csat_enabled": false, "config": config}
	var out Inbox
	if err := c.do(ctx, http.MethodPost, "/api/v1/inboxes", body, &out); err != nil {
		return 0, err
	}
	if out.ID == 0 {
		return 0, errors.New("libredesk: whatsapp create returned no id")
	}
	return out.ID, nil
}

// GetWhatsApp returns a WhatsApp inbox without its secrets. A missing inbox
// yields ErrNotFound.
func (c *Client) GetWhatsApp(ctx context.Context, id int64) (WhatsApp, error) {
	inbox, config, err := c.channelInbox(ctx, id, "whatsapp")
	if err != nil {
		return WhatsApp{}, err
	}
	name, _ := inbox["name"].(string)
	phone, _ := config["phone_number_id"].(string)
	waba, _ := config["waba_id"].(string)
	return WhatsApp{Name: name, PhoneNumberID: phone, WABAID: waba}, nil
}

// UpdateWhatsApp writes w into a WhatsApp inbox, keeping its other settings.
// A missing inbox yields ErrNotFound.
func (c *Client) UpdateWhatsApp(ctx context.Context, id int64, w WhatsApp, verifyToken string) error {
	inbox, config, err := c.channelInbox(ctx, id, "whatsapp")
	if err != nil {
		return err
	}
	inbox["name"] = w.Name
	applyWhatsApp(config, w, verifyToken)
	return c.do(ctx, http.MethodPut, "/api/v1/inboxes/"+strconv.FormatInt(id, 10), inbox, nil)
}
