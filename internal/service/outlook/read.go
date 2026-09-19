package outlook

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jhyoong/KumaApprove/internal/htmlutil"
	"github.com/jhyoong/KumaApprove/internal/service"
)

// MessageSummary holds a brief overview of an Outlook message.
type MessageSummary struct {
	ID      string `json:"id"`
	From    string `json:"from"`
	Subject string `json:"subject"`
	Date    string `json:"date"`
	Snippet string `json:"snippet"`
	IsRead  bool   `json:"isRead"`
}

// MessageDetail holds the full content of an Outlook message.
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

// graphMessageListResponse represents the Microsoft Graph list messages response.
type graphMessageListResponse struct {
	Value []graphMessage `json:"value"`
}

// graphMessage represents a single Microsoft Graph message.
type graphMessage struct {
	ID               string `json:"id"`
	Subject          string `json:"subject"`
	ReceivedDateTime string `json:"receivedDateTime"`
	BodyPreview      string `json:"bodyPreview"`
	IsRead           bool   `json:"isRead"`
	From             struct {
		EmailAddress struct {
			Address string `json:"address"`
		} `json:"emailAddress"`
	} `json:"from"`
	ToRecipients []struct {
		EmailAddress struct {
			Address string `json:"address"`
		} `json:"emailAddress"`
	} `json:"toRecipients"`
	Body struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
}

// list fetches recent messages. Uses default limit of 20 if not specified.
func (o *OutlookService) list(args map[string]string) (*service.Result, error) {
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

	endpoint := fmt.Sprintf("%s/v1.0/me/messages", o.baseURL)
	params := url.Values{}
	params.Set("$top", strconv.Itoa(limit))
	params.Set("$orderby", "receivedDateTime desc")
	fullURL := endpoint + "?" + params.Encode()

	messages, err := o.fetchMessages(fullURL)
	if err != nil {
		return nil, err
	}

	summaries := make([]MessageSummary, len(messages))
	for i, msg := range messages {
		summaries[i] = MessageSummary{
			ID:      msg.ID,
			From:    msg.From.EmailAddress.Address,
			Subject: msg.Subject,
			Date:    msg.ReceivedDateTime,
			Snippet: msg.BodyPreview,
			IsRead:  msg.IsRead,
		}
	}

	return &service.Result{Data: summaries}, nil
}

// get fetches a single message by ID. Requires the "id" parameter.
func (o *OutlookService) get(args map[string]string) (*service.Result, error) {
	id, ok := args["id"]
	if !ok || id == "" {
		return nil, fmt.Errorf("missing required parameter: id")
	}

	endpoint := fmt.Sprintf("%s/v1.0/me/messages/%s", o.baseURL, id)

	req, err := o.authedRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getting message %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get message %s: status %d", id, resp.StatusCode)
	}

	var msg graphMessage
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		return nil, fmt.Errorf("decoding message %s: %w", id, err)
	}

	to := ""
	if len(msg.ToRecipients) > 0 {
		to = msg.ToRecipients[0].EmailAddress.Address
	}

	body := msg.Body.Content
	bodyHTML := ""
	if strings.EqualFold(msg.Body.ContentType, "html") || msg.Body.ContentType == "" {
		bodyHTML = body
		body = htmlutil.StripTags(body)
	}

	return &service.Result{Data: MessageDetail{
		ID:       msg.ID,
		From:     msg.From.EmailAddress.Address,
		To:       to,
		Subject:  msg.Subject,
		Date:     msg.ReceivedDateTime,
		Snippet:  msg.BodyPreview,
		Body:     body,
		BodyHTML: bodyHTML,
	}}, nil
}

// search fetches messages matching a query. Requires the "query" parameter.
func (o *OutlookService) search(args map[string]string) (*service.Result, error) {
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

	endpoint := fmt.Sprintf("%s/v1.0/me/messages", o.baseURL)
	params := url.Values{}
	params.Set("$top", strconv.Itoa(limit))
	params.Set("$search", fmt.Sprintf("%q", query))
	fullURL := endpoint + "?" + params.Encode()

	messages, err := o.fetchMessages(fullURL)
	if err != nil {
		return nil, err
	}

	summaries := make([]MessageSummary, len(messages))
	for i, msg := range messages {
		summaries[i] = MessageSummary{
			ID:      msg.ID,
			From:    msg.From.EmailAddress.Address,
			Subject: msg.Subject,
			Date:    msg.ReceivedDateTime,
			Snippet: msg.BodyPreview,
			IsRead:  msg.IsRead,
		}
	}

	return &service.Result{Data: summaries}, nil
}

// fetchMessages calls the given Microsoft Graph endpoint and returns messages.
func (o *OutlookService) fetchMessages(fullURL string) ([]graphMessage, error) {
	req, err := o.authedRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing messages: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list messages: status %d", resp.StatusCode)
	}

	var listResp graphMessageListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("decoding list response: %w", err)
	}

	return listResp.Value, nil
}
