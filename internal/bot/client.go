package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const defaultBaseURL = "https://platform-api.max.ru"

// Client wraps the MAX Bot platform HTTP API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		baseURL:    defaultBaseURL,
		token:      token,
		httpClient: &http.Client{},
	}
}

func (c *Client) doRequest(method, path string, body any) ([]byte, error) {
	u, _ := url.Parse(c.baseURL + path)

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, u.String(), reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	// MAX platform API: token passed directly as Authorization header value (no "Bearer" prefix).
	req.Header.Set("Authorization", c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func (c *Client) SendMessage(chatID int64, text string, keyboard *InlineKeyboard) error {
	msg := sendMessageRequest{
		Text:   text,
		Format: "markdown",
	}
	if keyboard != nil {
		msg.Attachments = []attachment{{
			Type:    "inline_keyboard",
			Payload: keyboard,
		}}
	}
	// MAX API requires chat_id as a query parameter, not in the body.
	_, err := c.doRequest("POST", fmt.Sprintf("/messages?chat_id=%d", chatID), msg)
	return err
}

func (c *Client) SetWebhook(webhookURL string) error {
	_, err := c.doRequest("POST", "/subscriptions", map[string]any{
		"url":          webhookURL,
		"update_types": []string{"message_created", "message_callback", "bot_started"},
	})
	return err
}

func (c *Client) AnswerCallback(callbackID string) error {
	// MAX API: callback_id goes as a query parameter, body can be empty.
	_, err := c.doRequest("POST", fmt.Sprintf("/answers?callback_id=%s", callbackID), nil)
	return err
}

func (c *Client) GetMe() (json.RawMessage, error) {
	data, err := c.doRequest("GET", "/me", nil)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// DownloadFileGET performs GET on fileURL and returns body (Authorization: bot token as for platform API).
func (c *Client) DownloadFileGET(ctx context.Context, fileURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slurp, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download %d: %s", resp.StatusCode, string(slurp))
	}
	return io.ReadAll(resp.Body)
}

// DownloadUserFileInMax fetches a file the user sent to the bot, using url or token from the attachment payload.
func (c *Client) DownloadUserFileInMax(ctx context.Context, fileURL, token string) ([]byte, error) {
	if u := strings.TrimSpace(fileURL); u != "" {
		return c.DownloadFileGET(ctx, u)
	}
	if t := strings.TrimSpace(token); t != "" {
		for _, p := range []string{
			"/files/" + url.PathEscape(t),
			"/files?token=" + url.QueryEscape(t),
		} {
			if data, err := c.doRequestBytes(ctx, http.MethodGet, p, nil); err == nil {
				return data, nil
			}
		}
	}
	return nil, fmt.Errorf("no file url or downloadable token in message")
}

// doRequestBytes is like doRequest but returns body as bytes (used for file binary responses).
func (c *Client) doRequestBytes(ctx context.Context, method, path string, body any) ([]byte, error) {
	u, _ := url.Parse(c.baseURL + path)
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, rerr := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(b))
	}
	return b, rerr
}

// --- Request / response types for the MAX platform API ---

type sendMessageRequest struct {
	Text        string       `json:"text"`
	Format      string       `json:"format,omitempty"`
	Attachments []attachment `json:"attachments,omitempty"`
}

type attachment struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

type InlineKeyboard struct {
	Buttons [][]Button `json:"buttons"`
}

type Button struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Payload string `json:"payload,omitempty"`
	URL     string `json:"url,omitempty"`
}

// --- Webhook update types ---

type Update struct {
	UpdateType string    `json:"update_type"`
	Timestamp  int64     `json:"timestamp"`
	Message    *Message  `json:"message,omitempty"`
	Callback   *Callback `json:"callback,omitempty"`
	ChatID     int64     `json:"chat_id,omitempty"`
	User       *User     `json:"user,omitempty"`
	Payload    string    `json:"payload,omitempty"`
}

type Message struct {
	Sender    User      `json:"sender"`
	Recipient Recipient `json:"recipient"`
	Body      Body      `json:"body"`
	Timestamp int64     `json:"timestamp"`
}

type User struct {
	UserID   int64  `json:"user_id"`
	Name     string `json:"name"`
	Username string `json:"username"`
}

type Recipient struct {
	ChatID   int64  `json:"chat_id"`
	ChatType string `json:"chat_type"`
}

type Body struct {
	Mid         string          `json:"mid"`
	Seq         int64           `json:"seq"`
	Text        string          `json:"text"`
	Attachments json.RawMessage `json:"attachments,omitempty"`
}

type Callback struct {
	CallbackID string   `json:"callback_id"`
	Payload    string   `json:"payload"`
	User       User     `json:"user"`
	Message    *Message `json:"message,omitempty"`
}
