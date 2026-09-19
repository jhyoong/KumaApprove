package gmail

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jhyoong/KumaApprove/internal/htmlutil"
	"github.com/jhyoong/KumaApprove/internal/service"
)

// MessageSummary holds a brief overview of a Gmail message.
type MessageSummary struct {
	ID      string `json:"id"`
	From    string `json:"from"`
	Subject string `json:"subject"`
	Date    string `json:"date"`
	Snippet string `json:"snippet"`
}

// MessageDetail holds the full content of a Gmail message.
type MessageDetail struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	To       string `json:"to"`
	Subject  string `json:"subject"`
	Date     string `json:"date"`
	Snippet  string `json:"snippet"`
	Body     string `json:"body"`
	BodyHTML string `json:"bodyHtml,omitempty"`
}

// gmailListResponse represents the Gmail API list messages response.
type gmailListResponse struct {
	Messages []struct {
		ID       string `json:"id"`
		ThreadID string `json:"threadId"`
	} `json:"messages"`
}

// gmailPart represents a single MIME part of a Gmail message, which may
// itself contain nested parts (e.g. multipart/alternative inside multipart/mixed).
type gmailPart struct {
	MimeType string `json:"mimeType"`
	Body     struct {
		Data string `json:"data"`
	} `json:"body"`
	Parts []gmailPart `json:"parts"`
}

// gmailMessage represents a single Gmail API message response.
type gmailMessage struct {
	ID      string `json:"id"`
	Snippet string `json:"snippet"`
	Payload struct {
		MimeType string `json:"mimeType"`
		Headers  []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"headers"`
		Body struct {
			Data string `json:"data"`
		} `json:"body"`
		Parts []gmailPart `json:"parts"`
	} `json:"payload"`
}

// list fetches recent messages. Uses default limit of 20 if not specified.
func (g *GmailService) list(args map[string]string) (*service.Result, error) {
	limit := 20
	if l, ok := args["limit"]; ok {
		n, err := strconv.Atoi(l)
		if err != nil {
			return nil, fmt.Errorf("invalid limit: %w", err)
		}
		if n <= 0 || n > 500 {
			return nil, fmt.Errorf("limit must be between 1 and 500")
		}
		limit = n
	}

	ids, err := g.fetchMessageIDs("", limit)
	if err != nil {
		return nil, err
	}

	summaries, err := g.fetchSummaries(ids)
	if err != nil {
		return nil, err
	}

	return &service.Result{Data: summaries}, nil
}

// get fetches a single message by ID. Requires the "id" parameter.
func (g *GmailService) get(args map[string]string) (*service.Result, error) {
	id, ok := args["id"]
	if !ok || id == "" {
		return nil, fmt.Errorf("missing required parameter: id")
	}

	msg, err := g.fetchFullMessage(id)
	if err != nil {
		return nil, err
	}

	return &service.Result{Data: msg}, nil
}

// search fetches messages matching a query. Requires the "query" parameter.
func (g *GmailService) search(args map[string]string) (*service.Result, error) {
	query, ok := args["query"]
	if !ok || query == "" {
		return nil, fmt.Errorf("missing required parameter: query")
	}

	limit := 20
	if l, ok := args["limit"]; ok {
		n, err := strconv.Atoi(l)
		if err != nil {
			return nil, fmt.Errorf("invalid limit: %w", err)
		}
		if n <= 0 || n > 500 {
			return nil, fmt.Errorf("limit must be between 1 and 500")
		}
		limit = n
	}

	ids, err := g.fetchMessageIDs(query, limit)
	if err != nil {
		return nil, err
	}

	summaries, err := g.fetchSummaries(ids)
	if err != nil {
		return nil, err
	}

	return &service.Result{Data: summaries}, nil
}

// fetchMessageIDs calls the Gmail list endpoint and returns message IDs.
func (g *GmailService) fetchMessageIDs(query string, maxResults int) ([]string, error) {
	endpoint := fmt.Sprintf("%s/gmail/v1/users/me/messages", g.baseURL)
	params := url.Values{}
	params.Set("maxResults", strconv.Itoa(maxResults))
	if query != "" {
		params.Set("q", query)
	}
	fullURL := endpoint + "?" + params.Encode()

	req, err := g.authedRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing messages: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list messages: status %d", resp.StatusCode)
	}

	var listResp gmailListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("decoding list response: %w", err)
	}

	ids := make([]string, len(listResp.Messages))
	for i, m := range listResp.Messages {
		ids[i] = m.ID
	}
	return ids, nil
}

// fetchSummaries fetches metadata for each message ID and returns summaries.
func (g *GmailService) fetchSummaries(ids []string) ([]MessageSummary, error) {
	summaries := make([]MessageSummary, 0, len(ids))
	for _, id := range ids {
		endpoint := fmt.Sprintf("%s/gmail/v1/users/me/messages/%s?format=metadata", g.baseURL, id)
		req, err := g.authedRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}

		resp, err := g.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("getting message %s: %w", id, err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("get message %s: status %d", id, resp.StatusCode)
		}

		var msg gmailMessage
		err = json.NewDecoder(resp.Body).Decode(&msg)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decoding message %s: %w", id, err)
		}

		summaries = append(summaries, MessageSummary{
			ID:      msg.ID,
			From:    getHeader(msg, "From"),
			Subject: getHeader(msg, "Subject"),
			Date:    getHeader(msg, "Date"),
			Snippet: msg.Snippet,
		})
	}
	return summaries, nil
}

// fetchFullMessage fetches a complete message by ID.
func (g *GmailService) fetchFullMessage(id string) (MessageDetail, error) {
	endpoint := fmt.Sprintf("%s/gmail/v1/users/me/messages/%s?format=full", g.baseURL, id)
	req, err := g.authedRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return MessageDetail{}, err
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return MessageDetail{}, fmt.Errorf("getting message %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return MessageDetail{}, fmt.Errorf("get message %s: status %d", id, resp.StatusCode)
	}

	var msg gmailMessage
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		return MessageDetail{}, fmt.Errorf("decoding message %s: %w", id, err)
	}

	plain, htmlBody := extractBodies(msg)
	return MessageDetail{
		ID:       msg.ID,
		From:     getHeader(msg, "From"),
		To:       getHeader(msg, "To"),
		Subject:  getHeader(msg, "Subject"),
		Date:     getHeader(msg, "Date"),
		Snippet:  msg.Snippet,
		Body:     plain,
		BodyHTML: htmlBody,
	}, nil
}

// getHeader extracts a header value by name from a Gmail message.
func getHeader(msg gmailMessage, name string) string {
	for _, h := range msg.Payload.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

// extractBodies extracts the plain text and HTML bodies from a Gmail message.
// Gmail uses URL-safe base64 encoding, sometimes without padding.
func extractBodies(msg gmailMessage) (plain, htmlBody string) {
	plain, htmlBody = findBodies(msg.Payload.Parts)

	if plain == "" && htmlBody == "" && msg.Payload.Body.Data != "" {
		decoded, err := decodeBase64URL(msg.Payload.Body.Data)
		if err == nil {
			if msg.Payload.MimeType == "text/html" {
				htmlBody = decoded
			} else {
				plain = decoded
			}
		}
	}

	if plain == "" && htmlBody != "" {
		plain = htmlutil.StripTags(htmlBody)
	}

	return plain, htmlBody
}

// findBodies recursively searches MIME parts for text/plain and text/html bodies.
func findBodies(parts []gmailPart) (plain, htmlBody string) {
	for _, part := range parts {
		if len(part.Parts) > 0 {
			p, h := findBodies(part.Parts)
			if plain == "" {
				plain = p
			}
			if htmlBody == "" {
				htmlBody = h
			}
		}

		if part.Body.Data == "" {
			continue
		}
		decoded, err := decodeBase64URL(part.Body.Data)
		if err != nil {
			continue
		}

		switch part.MimeType {
		case "text/plain":
			if plain == "" {
				plain = decoded
			}
		case "text/html":
			if htmlBody == "" {
				htmlBody = decoded
			}
		}
	}
	return plain, htmlBody
}

// decodeBase64URL decodes a URL-safe base64 string, handling missing padding.
func decodeBase64URL(s string) (string, error) {
	s = strings.TrimRight(s, "=")
	decoded, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}
