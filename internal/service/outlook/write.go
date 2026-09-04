package outlook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jhyoong/KumaApprove/internal/service"
)

// send sends a new email via the Microsoft Graph API.
func (o *OutlookService) send(args map[string]string) (*service.Result, error) {
	to, ok := args["to"]
	if !ok || to == "" {
		return nil, fmt.Errorf("missing required parameter: to")
	}
	subject, ok := args["subject"]
	if !ok || subject == "" {
		return nil, fmt.Errorf("missing required parameter: subject")
	}
	body := args["body"]

	payload := map[string]any{
		"message": map[string]any{
			"subject": subject,
			"body": map[string]string{
				"contentType": "Text",
				"content":     body,
			},
			"toRecipients": []map[string]any{
				{
					"emailAddress": map[string]string{
						"address": to,
					},
				},
			},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling send payload: %w", err)
	}

	endpoint := fmt.Sprintf("%s/v1.0/me/sendMail", o.baseURL)
	req, err := o.authedRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending email: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("send email: status %d", resp.StatusCode)
	}

	return &service.Result{Data: map[string]string{"status": "sent"}}, nil
}

// reply replies to an existing message via the Microsoft Graph API.
func (o *OutlookService) reply(args map[string]string) (*service.Result, error) {
	id, ok := args["id"]
	if !ok || id == "" {
		return nil, fmt.Errorf("missing required parameter: id")
	}
	body, ok := args["body"]
	if !ok || body == "" {
		return nil, fmt.Errorf("missing required parameter: body")
	}

	payload := map[string]string{
		"comment": body,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling reply payload: %w", err)
	}

	endpoint := fmt.Sprintf("%s/v1.0/me/messages/%s/reply", o.baseURL, id)
	req, err := o.authedRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("replying to message %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("reply to message %s: status %d", id, resp.StatusCode)
	}

	return &service.Result{Data: map[string]string{"status": "replied"}}, nil
}

// draft creates a draft email via the Microsoft Graph API.
func (o *OutlookService) draft(args map[string]string) (*service.Result, error) {
	to, ok := args["to"]
	if !ok || to == "" {
		return nil, fmt.Errorf("missing required parameter: to")
	}
	subject, ok := args["subject"]
	if !ok || subject == "" {
		return nil, fmt.Errorf("missing required parameter: subject")
	}
	body := args["body"]

	payload := map[string]any{
		"subject": subject,
		"body": map[string]string{
			"contentType": "Text",
			"content":     body,
		},
		"toRecipients": []map[string]any{
			{
				"emailAddress": map[string]string{
					"address": to,
				},
			},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling draft payload: %w", err)
	}

	endpoint := fmt.Sprintf("%s/v1.0/me/messages", o.baseURL)
	req, err := o.authedRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("creating draft: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("create draft: status %d", resp.StatusCode)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding draft response: %w", err)
	}

	return &service.Result{Data: map[string]string{"draft_id": result.ID}}, nil
}
