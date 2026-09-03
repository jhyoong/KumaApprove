package approval

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TelegramConfig holds configuration for the Telegram approval bot.
type TelegramConfig struct {
	BotToken     string
	ChatID       string
	BaseURL      string
	PollInterval time.Duration
}

// TelegramApprover sends approval requests via Telegram inline keyboards
// and polls for callback query responses.
type TelegramApprover struct {
	config   TelegramConfig
	client   *http.Client
	updateID int64
}

// NewTelegramApprover creates a TelegramApprover with sensible defaults.
func NewTelegramApprover(cfg TelegramConfig) *TelegramApprover {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.telegram.org"
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 2 * time.Second
	}
	return &TelegramApprover{
		config: cfg,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// RequestApproval sends a Telegram message with Approve/Reject inline buttons,
// then polls getUpdates until a matching callback is received or the context expires.
func (t *TelegramApprover) RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalResult, error) {
	if req.RequestID == "" {
		req.RequestID = generateRequestID()
	}

	messageID, err := t.sendApprovalMessage(req)
	if err != nil {
		return ApprovalResult{}, fmt.Errorf("sending telegram message: %w", err)
	}

	return t.pollForResponse(ctx, req.RequestID, messageID)
}

// sendApprovalMessage formats and sends the approval message with inline keyboard.
func (t *TelegramApprover) sendApprovalMessage(req ApprovalRequest) (int64, error) {
	text := formatApprovalMessage(req)

	keyboard := map[string]any{
		"inline_keyboard": [][]map[string]string{
			{
				{"text": "Approve", "callback_data": fmt.Sprintf("approve:%s", req.RequestID)},
				{"text": "Reject", "callback_data": fmt.Sprintf("reject:%s", req.RequestID)},
			},
		},
	}
	kbJSON, _ := json.Marshal(keyboard)

	params := url.Values{
		"chat_id":      {t.config.ChatID},
		"text":         {text},
		"parse_mode":   {"Markdown"},
		"reply_markup": {string(kbJSON)},
	}

	resp, err := t.apiCall("sendMessage", params)
	if err != nil {
		return 0, err
	}

	msgID, _ := resp["message_id"].(float64)
	return int64(msgID), nil
}

// pollForResponse polls getUpdates at the configured interval until a matching
// callback query is found or the context is cancelled.
func (t *TelegramApprover) pollForResponse(ctx context.Context, requestID string, messageID int64) (ApprovalResult, error) {
	ticker := time.NewTicker(t.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ApprovalResult{}, fmt.Errorf("approval timed out")
		case <-ticker.C:
			result, found, err := t.checkUpdates(requestID, messageID)
			if err != nil {
				continue
			}
			if found {
				return result, nil
			}
		}
	}
}

// checkUpdates fetches new updates from Telegram and checks for a matching callback query.
func (t *TelegramApprover) checkUpdates(requestID string, messageID int64) (ApprovalResult, bool, error) {
	params := url.Values{
		"timeout": {"1"},
	}
	if t.updateID > 0 {
		params.Set("offset", fmt.Sprintf("%d", t.updateID+1))
	}

	raw, err := t.apiCallRaw("getUpdates", params)
	if err != nil {
		return ApprovalResult{}, false, err
	}

	var body struct {
		OK     bool `json:"ok"`
		Result []struct {
			UpdateID      int64 `json:"update_id"`
			CallbackQuery *struct {
				ID      string `json:"id"`
				Data    string `json:"data"`
				Message struct {
					MessageID int64 `json:"message_id"`
					Chat      struct {
						ID int64 `json:"id"`
					} `json:"chat"`
				} `json:"message"`
			} `json:"callback_query"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return ApprovalResult{}, false, err
	}

	for _, update := range body.Result {
		t.updateID = update.UpdateID
		if update.CallbackQuery == nil {
			continue
		}

		data := update.CallbackQuery.Data
		cbID := update.CallbackQuery.ID

		var approved bool
		var matchedID string

		if strings.HasPrefix(data, "approve:") {
			matchedID = strings.TrimPrefix(data, "approve:")
			approved = true
		} else if strings.HasPrefix(data, "reject:") {
			matchedID = strings.TrimPrefix(data, "reject:")
			approved = false
		} else {
			continue
		}

		if matchedID != requestID {
			continue
		}

		t.answerCallback(cbID, approved)
		t.removeKeyboard(messageID)

		msg := "Approved"
		if !approved {
			msg = "Rejected"
		}
		return ApprovalResult{Approved: approved, Message: msg}, true, nil
	}

	return ApprovalResult{}, false, nil
}

// answerCallback acknowledges a callback query with a text response.
func (t *TelegramApprover) answerCallback(callbackID string, approved bool) {
	text := "Approved"
	if !approved {
		text = "Rejected"
	}
	t.apiCall("answerCallbackQuery", url.Values{
		"callback_query_id": {callbackID},
		"text":              {text},
	})
}

// removeKeyboard removes the inline keyboard from a message after a response.
func (t *TelegramApprover) removeKeyboard(messageID int64) {
	t.apiCall("editMessageReplyMarkup", url.Values{
		"chat_id":      {t.config.ChatID},
		"message_id":   {fmt.Sprintf("%d", messageID)},
		"reply_markup": {`{"inline_keyboard":[]}`},
	})
}

// apiCall makes a POST request to the Telegram Bot API and returns the result object.
func (t *TelegramApprover) apiCall(method string, params url.Values) (map[string]any, error) {
	raw, err := t.apiCallRaw(method, params)
	if err != nil {
		return nil, err
	}
	var body struct {
		OK     bool           `json:"ok"`
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	if !body.OK {
		return nil, fmt.Errorf("telegram API error: %s", string(raw))
	}
	return body.Result, nil
}

// apiCallRaw makes a POST request to the Telegram Bot API and returns the raw response body.
func (t *TelegramApprover) apiCallRaw(method string, params url.Values) ([]byte, error) {
	apiURL := fmt.Sprintf("%s/bot%s/%s", t.config.BaseURL, t.config.BotToken, method)
	resp, err := t.client.PostForm(apiURL, params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// SendMessage sends a plain text notification via Telegram (e.g. for AUTH_EXPIRED alerts).
func (t *TelegramApprover) SendMessage(text string) error {
	_, err := t.apiCall("sendMessage", url.Values{
		"chat_id":    {t.config.ChatID},
		"text":       {text},
		"parse_mode": {"Markdown"},
	})
	return err
}

// formatApprovalMessage builds a Markdown-formatted approval message.
func formatApprovalMessage(req ApprovalRequest) string {
	var b strings.Builder
	b.WriteString("*KumaApprove -- Approval Required*\n\n")
	b.WriteString(fmt.Sprintf("*Action:* `%s`\n", req.Action))
	if req.Account != "" {
		b.WriteString(fmt.Sprintf("*Account:* %s\n", req.Account))
	}
	b.WriteString(fmt.Sprintf("*Time:* %s\n", time.Now().UTC().Format("2006-01-02 15:04:05 UTC")))

	if len(req.Details) > 0 {
		b.WriteString("\n*Details:*\n")
		for k, v := range req.Details {
			display := v
			if len(display) > 500 {
				display = display[:500] + "..."
			}
			b.WriteString(fmt.Sprintf("  %s: %s\n", k, display))
		}
	}

	b.WriteString(fmt.Sprintf("\n*Request ID:* `%s`", req.RequestID))
	return b.String()
}

// generateRequestID creates a random 4-byte hex ID for tracking approval requests.
func generateRequestID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}
