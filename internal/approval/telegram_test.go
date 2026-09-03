package approval

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestTelegramSendAndApprove(t *testing.T) {
	var mu sync.Mutex
	var sentMessage bool
	requestID := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.URL.Path == "/botsecret/sendMessage":
			sentMessage = true
			r.ParseForm()
			replyMarkup := r.FormValue("reply_markup")
			var kb struct {
				InlineKeyboard [][]struct {
					Text         string `json:"text"`
					CallbackData string `json:"callback_data"`
				} `json:"inline_keyboard"`
			}
			json.Unmarshal([]byte(replyMarkup), &kb)
			if len(kb.InlineKeyboard) > 0 && len(kb.InlineKeyboard[0]) > 0 {
				// callback_data is "approve:{id}", extract the full string
				requestID = kb.InlineKeyboard[0][0].CallbackData
			}
			json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{
					"message_id": 42,
				},
			})

		case r.URL.Path == "/botsecret/getUpdates":
			if requestID != "" {
				json.NewEncoder(w).Encode(map[string]any{
					"ok": true,
					"result": []map[string]any{
						{
							"update_id": 1,
							"callback_query": map[string]any{
								"id":   "cb1",
								"data": requestID,
								"message": map[string]any{
									"message_id": 42,
									"chat":       map[string]any{"id": 123},
								},
							},
						},
					},
				})
			} else {
				json.NewEncoder(w).Encode(map[string]any{
					"ok":     true,
					"result": []any{},
				})
			}

		case r.URL.Path == "/botsecret/answerCallbackQuery":
			json.NewEncoder(w).Encode(map[string]any{"ok": true})

		case r.URL.Path == "/botsecret/editMessageReplyMarkup":
			json.NewEncoder(w).Encode(map[string]any{"ok": true})

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tg := NewTelegramApprover(TelegramConfig{
		BotToken:     "secret",
		ChatID:       "123",
		BaseURL:      server.URL,
		PollInterval: 100 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := tg.RequestApproval(ctx, ApprovalRequest{
		Action:  "gmail:send",
		Account: "test@gmail.com",
		Details: map[string]string{
			"to":      "boss@company.com",
			"subject": "Test",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Approved {
		t.Fatal("expected approved")
	}
	mu.Lock()
	if !sentMessage {
		t.Fatal("expected message to be sent")
	}
	mu.Unlock()
}

func TestTelegramReject(t *testing.T) {
	var mu sync.Mutex
	requestID := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.URL.Path == "/botsecret/sendMessage":
			r.ParseForm()
			replyMarkup := r.FormValue("reply_markup")
			var kb struct {
				InlineKeyboard [][]struct {
					Text         string `json:"text"`
					CallbackData string `json:"callback_data"`
				} `json:"inline_keyboard"`
			}
			json.Unmarshal([]byte(replyMarkup), &kb)
			// Capture the reject callback_data (second button)
			if len(kb.InlineKeyboard) > 0 && len(kb.InlineKeyboard[0]) > 1 {
				requestID = kb.InlineKeyboard[0][1].CallbackData
			}
			json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": map[string]any{"message_id": 42},
			})
		case r.URL.Path == "/botsecret/getUpdates":
			if requestID != "" {
				json.NewEncoder(w).Encode(map[string]any{
					"ok": true,
					"result": []map[string]any{
						{
							"update_id": 1,
							"callback_query": map[string]any{
								"id":   "cb1",
								"data": requestID,
								"message": map[string]any{
									"message_id": 42,
									"chat":       map[string]any{"id": 123},
								},
							},
						},
					},
				})
			} else {
				json.NewEncoder(w).Encode(map[string]any{
					"ok":     true,
					"result": []any{},
				})
			}
		case r.URL.Path == "/botsecret/answerCallbackQuery":
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case r.URL.Path == "/botsecret/editMessageReplyMarkup":
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tg := NewTelegramApprover(TelegramConfig{
		BotToken:     "secret",
		ChatID:       "123",
		BaseURL:      server.URL,
		PollInterval: 100 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := tg.RequestApproval(ctx, ApprovalRequest{
		Action:  "exec:run",
		Details: map[string]string{"cmd": "ls"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Approved {
		t.Fatal("expected rejected")
	}
}

func TestTelegramTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/botsecret/sendMessage":
			json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": map[string]any{"message_id": 42},
			})
		case r.URL.Path == "/botsecret/getUpdates":
			json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": []any{},
			})
		default:
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	defer server.Close()

	tg := NewTelegramApprover(TelegramConfig{
		BotToken:     "secret",
		ChatID:       "123",
		BaseURL:      server.URL,
		PollInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	_, err := tg.RequestApproval(ctx, ApprovalRequest{
		Action: "gmail:send",
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
