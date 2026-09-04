package gmail

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jhyoong/KumaApprove/internal/service"
)

// originalMessage holds the fields needed from an original message for reply threading.
type originalMessage struct {
	ThreadID  string
	From      string
	Subject   string
	MessageID string
}

// send sends a new email. Requires "to", "subject", and "body" parameters.
func (g *GmailService) send(args map[string]string) (*service.Result, error) {
	to := args["to"]
	if to == "" {
		return nil, fmt.Errorf("missing required parameter: to")
	}
	subject := args["subject"]
	if subject == "" {
		return nil, fmt.Errorf("missing required parameter: subject")
	}
	body := args["body"]

	raw := buildRawEmail(to, subject, body, "", "")
	return g.sendRaw(raw, "")
}

// reply sends a reply to an existing message. Requires "id" and "body" parameters.
func (g *GmailService) reply(args map[string]string) (*service.Result, error) {
	id := args["id"]
	if id == "" {
		return nil, fmt.Errorf("missing required parameter: id")
	}
	body := args["body"]
	if body == "" {
		return nil, fmt.Errorf("missing required parameter: body")
	}

	orig, err := g.fetchOriginalForReply(id)
	if err != nil {
		return nil, fmt.Errorf("fetching original message: %w", err)
	}

	subject := orig.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}

	raw := buildRawEmail(orig.From, subject, body, orig.MessageID, orig.MessageID)
	return g.sendRaw(raw, orig.ThreadID)
}

// draft creates a draft email. Requires "to", "subject", and "body" parameters.
func (g *GmailService) draft(args map[string]string) (*service.Result, error) {
	to := args["to"]
	if to == "" {
		return nil, fmt.Errorf("missing required parameter: to")
	}
	subject := args["subject"]
	if subject == "" {
		return nil, fmt.Errorf("missing required parameter: subject")
	}
	body := args["body"]

	raw := buildRawEmail(to, subject, body, "", "")
	encoded := base64.RawURLEncoding.EncodeToString([]byte(raw))

	payload := map[string]any{
		"message": map[string]any{
			"raw": encoded,
		},
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encoding draft payload: %w", err)
	}

	endpoint := fmt.Sprintf("%s/gmail/v1/users/me/drafts", g.baseURL)
	req, err := g.authedRequest(http.MethodPost, endpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("creating draft: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("create draft: status %d", resp.StatusCode)
	}

	var draftResp struct {
		ID      string `json:"id"`
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&draftResp); err != nil {
		return nil, fmt.Errorf("decoding draft response: %w", err)
	}

	return &service.Result{
		Data: map[string]string{
			"draft_id": draftResp.ID,
		},
	}, nil
}

// sendRaw sends a base64url-encoded raw email via the Gmail API.
// If threadID is non-empty, it is included for threading.
func (g *GmailService) sendRaw(rawEmail string, threadID string) (*service.Result, error) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(rawEmail))

	payload := map[string]any{
		"raw": encoded,
	}
	if threadID != "" {
		payload["threadId"] = threadID
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encoding send payload: %w", err)
	}

	endpoint := fmt.Sprintf("%s/gmail/v1/users/me/messages/send", g.baseURL)
	req, err := g.authedRequest(http.MethodPost, endpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("send message: status %d", resp.StatusCode)
	}

	var sendResp struct {
		ID       string `json:"id"`
		ThreadID string `json:"threadId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sendResp); err != nil {
		return nil, fmt.Errorf("decoding send response: %w", err)
	}

	return &service.Result{
		Data: map[string]string{
			"id": sendResp.ID,
		},
	}, nil
}

// fetchOriginalForReply fetches the thread ID, From, Subject, and Message-ID
// from the original message needed for constructing a reply.
func (g *GmailService) fetchOriginalForReply(id string) (originalMessage, error) {
	endpoint := fmt.Sprintf("%s/gmail/v1/users/me/messages/%s?format=metadata", g.baseURL, id)
	req, err := g.authedRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return originalMessage{}, err
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return originalMessage{}, fmt.Errorf("fetching message %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return originalMessage{}, fmt.Errorf("fetch message %s: status %d", id, resp.StatusCode)
	}

	var msg struct {
		ThreadID string `json:"threadId"`
		Payload  struct {
			Headers []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"headers"`
		} `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		return originalMessage{}, fmt.Errorf("decoding message %s: %w", id, err)
	}

	orig := originalMessage{
		ThreadID: msg.ThreadID,
	}
	for _, h := range msg.Payload.Headers {
		switch strings.ToLower(h.Name) {
		case "from":
			orig.From = h.Value
		case "subject":
			orig.Subject = h.Value
		case "message-id":
			orig.MessageID = h.Value
		}
	}

	return orig, nil
}

// buildRawEmail constructs an RFC 2822 email message.
// If inReplyTo and references are non-empty, threading headers are added.
func buildRawEmail(to, subject, body, inReplyTo, references string) string {
	var b strings.Builder
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	if inReplyTo != "" {
		b.WriteString("In-Reply-To: " + inReplyTo + "\r\n")
	}
	if references != "" {
		b.WriteString("References: " + references + "\r\n")
	}
	b.WriteString("\r\n")
	b.WriteString(body)
	return b.String()
}
