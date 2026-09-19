# HTML Email Body Extraction Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extract readable content from HTML-only emails in both Gmail and Outlook services, and expose both plain text and raw HTML body fields.

**Architecture:** Shared `internal/htmlutil` package with `StripTags` function that converts HTML to plain text using `golang.org/x/net/html` tokenizer. Gmail's MIME parser is updated to recurse into nested parts and extract `text/html`. Outlook's body handling branches on the Graph API's `contentType` field. Both services add a `bodyHtml` field to `MessageDetail`.

**Tech Stack:** Go 1.26.4, `golang.org/x/net/html` (already in go.sum as indirect dep)

**Design doc:** `docs/plans/2026-09-19-html-email-extraction-design.md`

---

### Task 1: Create `internal/htmlutil` package -- tests

**Files:**
- Create: `internal/htmlutil/strip_test.go`

**Step 1: Write failing tests for StripTags**

```go
package htmlutil

import "testing"

func TestStripTagsBasic(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text passthrough", "Hello World", "Hello World"},
		{"simple tags", "<b>bold</b> text", "bold text"},
		{"nested tags", "<div><p><b>deep</b></p></div>", "deep"},
		{"br newline", "line1<br>line2", "line1\nline2"},
		{"br self-closing", "line1<br/>line2", "line1\nline2"},
		{"paragraph breaks", "<p>First</p><p>Second</p>", "First\n\nSecond"},
		{"div breaks", "<div>A</div><div>B</div>", "A\n\nB"},
		{"list items", "<ul><li>one</li><li>two</li></ul>", "one\n\ntwo"},
		{"empty input", "", ""},
		{"whitespace only tags", "<p>  </p><p>  </p>", ""},
		{"html entities", "Tom &amp; Jerry &lt;3", "Tom & Jerry <3"},
		{"style excluded", "<style>.red{color:red}</style>Hello", "Hello"},
		{"script excluded", "<script>alert('xss')</script>Hello", "Hello"},
		{"collapse whitespace", "<p>  lots   of   spaces  </p>", "lots of spaces"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripTags(tt.input)
			if got != tt.want {
				t.Errorf("StripTags(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStripTagsRealisticEmail(t *testing.T) {
	html := `<html><body><p>Hi there,</p><p>Please review the attached document.</p><p>Thanks,<br>Alice</p></body></html>`
	got := StripTags(html)
	want := "Hi there,\n\nPlease review the attached document.\n\nThanks,\nAlice"
	if got != want {
		t.Errorf("StripTags realistic email:\ngot:  %q\nwant: %q", got, want)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/htmlutil/ -v`
Expected: FAIL -- package does not exist yet

**Step 3: Commit**

```bash
git add internal/htmlutil/strip_test.go
git commit -m "test: add StripTags tests for htmlutil package"
```

---

### Task 2: Create `internal/htmlutil` package -- implementation

**Files:**
- Create: `internal/htmlutil/strip.go`

**Step 1: Implement StripTags**

```go
package htmlutil

import (
	"strings"

	"golang.org/x/net/html"
)

var blockElements = map[string]bool{
	"p": true, "div": true, "br": true, "li": true, "tr": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"blockquote": true, "pre": true, "hr": true,
}

var skipElements = map[string]bool{
	"style": true, "script": true,
}

func StripTags(s string) string {
	if s == "" {
		return ""
	}
	tokenizer := html.NewTokenizer(strings.NewReader(s))
	var b strings.Builder
	skipDepth := 0

	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			return normalizeWhitespace(b.String())
		case html.StartTagToken, html.SelfClosingTagToken:
			tn, _ := tokenizer.TagName()
			tagName := string(tn)
			if skipElements[tagName] {
				skipDepth++
			}
			if blockElements[tagName] {
				b.WriteString("\n")
			}
		case html.EndTagToken:
			tn, _ := tokenizer.TagName()
			tagName := string(tn)
			if skipElements[tagName] && skipDepth > 0 {
				skipDepth--
			}
			if blockElements[tagName] {
				b.WriteString("\n")
			}
		case html.TextToken:
			if skipDepth == 0 {
				b.Write(tokenizer.Text())
			}
		}
	}
}

func normalizeWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	var result []string
	prevBlank := true
	for _, line := range lines {
		trimmed := collapseSpaces(strings.TrimSpace(line))
		if trimmed == "" {
			if !prevBlank && len(result) > 0 {
				result = append(result, "")
			}
			prevBlank = true
		} else {
			result = append(result, trimmed)
			prevBlank = false
		}
	}
	out := strings.Join(result, "\n")
	return strings.TrimSpace(out)
}

func collapseSpaces(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\r' {
			if !prevSpace {
				b.WriteRune(' ')
			}
			prevSpace = true
		} else {
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return b.String()
}
```

**Step 2: Run tests to verify they pass**

Run: `go test ./internal/htmlutil/ -v`
Expected: all PASS

If any test fails, adjust the implementation to match expected behavior. The `normalizeWhitespace` function collapses multiple blank lines into one and trims leading/trailing whitespace. Block elements emit `\n` at both start and end tags, producing blank lines between paragraphs (collapsed by `normalizeWhitespace`).

**Step 3: Commit**

```bash
git add internal/htmlutil/strip.go
git commit -m "feat: add htmlutil.StripTags for HTML-to-text conversion"
```

---

### Task 3: Update Gmail structs and body extraction -- tests

**Files:**
- Modify: `internal/service/gmail/read_test.go`

Add `encoding/base64` to the test file's imports (alongside existing `encoding/json`, `net/http`, `net/http/httptest`, `testing`).

**Step 1: Add a base64url helper and new test cases**

Add this helper at the top of the file (after `newTestService`):

```go
func b64(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}
```

Add these test functions after the existing `TestEnrichDetailsReplyAPIError`:

```go
func TestGetMessageHTMLOnly(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/v1/users/me/messages/html1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "html1",
			"snippet": "Hello from HTML",
			"payload": map[string]any{
				"mimeType": "text/html",
				"headers": []map[string]string{
					{"name": "From", "value": "alice@example.com"},
					{"name": "To", "value": "bob@example.com"},
					{"name": "Subject", "value": "HTML Only"},
					{"name": "Date", "value": "Mon, 1 Jan 2024 12:00:00 +0000"},
				},
				"body": map[string]string{
					"data": b64("<p>Hello World</p>"),
				},
			},
		})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("get", map[string]string{"id": "html1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	msg := result.Data.(MessageDetail)
	if msg.Body != "Hello World" {
		t.Errorf("expected Body='Hello World', got %q", msg.Body)
	}
	if msg.BodyHTML != "<p>Hello World</p>" {
		t.Errorf("expected BodyHTML='<p>Hello World</p>', got %q", msg.BodyHTML)
	}
}

func TestGetMessageMultipartHTMLOnly(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/v1/users/me/messages/mhtml1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "mhtml1",
			"snippet": "Multipart HTML",
			"payload": map[string]any{
				"mimeType": "multipart/alternative",
				"headers": []map[string]string{
					{"name": "From", "value": "alice@example.com"},
					{"name": "To", "value": "bob@example.com"},
					{"name": "Subject", "value": "Multipart HTML Only"},
					{"name": "Date", "value": "Mon, 1 Jan 2024 12:00:00 +0000"},
				},
				"body": map[string]string{"data": ""},
				"parts": []map[string]any{
					{
						"mimeType": "text/html",
						"body":     map[string]string{"data": b64("<div>HTML only</div>")},
					},
				},
			},
		})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("get", map[string]string{"id": "mhtml1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	msg := result.Data.(MessageDetail)
	if msg.Body != "HTML only" {
		t.Errorf("expected Body='HTML only', got %q", msg.Body)
	}
	if msg.BodyHTML != "<div>HTML only</div>" {
		t.Errorf("expected BodyHTML='<div>HTML only</div>', got %q", msg.BodyHTML)
	}
}

func TestGetMessageNestedParts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/v1/users/me/messages/nested1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "nested1",
			"snippet": "Nested",
			"payload": map[string]any{
				"mimeType": "multipart/mixed",
				"headers": []map[string]string{
					{"name": "From", "value": "alice@example.com"},
					{"name": "To", "value": "bob@example.com"},
					{"name": "Subject", "value": "Nested Parts"},
					{"name": "Date", "value": "Mon, 1 Jan 2024 12:00:00 +0000"},
				},
				"body": map[string]string{"data": ""},
				"parts": []map[string]any{
					{
						"mimeType": "multipart/alternative",
						"body":     map[string]string{"data": ""},
						"parts": []map[string]any{
							{
								"mimeType": "text/html",
								"body":     map[string]string{"data": b64("<b>Nested HTML</b>")},
							},
						},
					},
				},
			},
		})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("get", map[string]string{"id": "nested1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	msg := result.Data.(MessageDetail)
	if msg.Body != "Nested HTML" {
		t.Errorf("expected Body='Nested HTML', got %q", msg.Body)
	}
	if msg.BodyHTML != "<b>Nested HTML</b>" {
		t.Errorf("expected BodyHTML='<b>Nested HTML</b>', got %q", msg.BodyHTML)
	}
}

func TestGetMessageBothParts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/v1/users/me/messages/both1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "both1",
			"snippet": "Both parts",
			"payload": map[string]any{
				"mimeType": "multipart/alternative",
				"headers": []map[string]string{
					{"name": "From", "value": "alice@example.com"},
					{"name": "To", "value": "bob@example.com"},
					{"name": "Subject", "value": "Both Parts"},
					{"name": "Date", "value": "Mon, 1 Jan 2024 12:00:00 +0000"},
				},
				"body": map[string]string{"data": ""},
				"parts": []map[string]any{
					{
						"mimeType": "text/plain",
						"body":     map[string]string{"data": b64("Plain version")},
					},
					{
						"mimeType": "text/html",
						"body":     map[string]string{"data": b64("<p>HTML version</p>")},
					},
				},
			},
		})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("get", map[string]string{"id": "both1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	msg := result.Data.(MessageDetail)
	if msg.Body != "Plain version" {
		t.Errorf("expected Body='Plain version', got %q", msg.Body)
	}
	if msg.BodyHTML != "<p>HTML version</p>" {
		t.Errorf("expected BodyHTML='<p>HTML version</p>', got %q", msg.BodyHTML)
	}
}
```

**Step 2: Run tests to verify new ones fail**

Run: `go test ./internal/service/gmail/ -run "TestGetMessageHTMLOnly|TestGetMessageMultipartHTMLOnly|TestGetMessageNestedParts|TestGetMessageBothParts" -v`
Expected: FAIL -- `BodyHTML` field does not exist on `MessageDetail`

**Step 3: Commit**

```bash
git add internal/service/gmail/read_test.go
git commit -m "test: add Gmail tests for HTML-only and nested MIME parts"
```

---

### Task 4: Update Gmail structs and body extraction -- implementation

**Files:**
- Modify: `internal/service/gmail/read.go:1-287`

**Step 1: Add `BodyHTML` field to `MessageDetail`**

In `internal/service/gmail/read.go`, add the `BodyHTML` field to the `MessageDetail` struct (after the `Body` field at line 32):

```go
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
```

**Step 2: Replace inline `Parts` with recursive `gmailPart` struct**

Replace the current `gmailMessage` struct (lines 44-62) with:

```go
type gmailPart struct {
	MimeType string `json:"mimeType"`
	Body     struct {
		Data string `json:"data"`
	} `json:"body"`
	Parts []gmailPart `json:"parts"`
}

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
```

**Step 3: Add `htmlutil` import**

Add to the import block:

```go
"github.com/jhyoong/KumaApprove/internal/htmlutil"
```

**Step 4: Replace `extractBody` with `extractBodies` and `findBodies`**

Remove the existing `extractBody` function (lines 259-277) and replace with:

```go
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
```

**Step 5: Update `fetchFullMessage` to use `extractBodies`**

In `fetchFullMessage` (around line 236-244), replace:

```go
	return MessageDetail{
		ID:      msg.ID,
		From:    getHeader(msg, "From"),
		To:      getHeader(msg, "To"),
		Subject: getHeader(msg, "Subject"),
		Date:    getHeader(msg, "Date"),
		Snippet: msg.Snippet,
		Body:    extractBody(msg),
	}, nil
```

with:

```go
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
```

**Step 6: Run all Gmail tests**

Run: `go test ./internal/service/gmail/ -v`
Expected: all PASS (both old and new tests)

**Step 7: Commit**

```bash
git add internal/service/gmail/read.go
git commit -m "feat(gmail): extract HTML body and support nested MIME parts"
```

---

### Task 5: Update Outlook structs and body handling -- tests

**Files:**
- Modify: `internal/service/outlook/read_test.go`

**Step 1: Add new test cases**

Add these test functions after the existing `TestEnrichDetailsReplyAPIError`:

```go
func TestGetMessageHTMLBody(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/me/messages/html1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":               "html1",
			"subject":          "HTML Email",
			"receivedDateTime": "2024-01-01T12:00:00Z",
			"bodyPreview":      "Hello World",
			"isRead":           true,
			"from": map[string]any{
				"emailAddress": map[string]string{"address": "alice@example.com"},
			},
			"toRecipients": []map[string]any{
				{"emailAddress": map[string]string{"address": "bob@example.com"}},
			},
			"body": map[string]any{
				"contentType": "html",
				"content":     "<p>Hello World</p>",
			},
		})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("get", map[string]string{"id": "html1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	msg := result.Data.(MessageDetail)
	if msg.Body != "Hello World" {
		t.Errorf("expected Body='Hello World', got %q", msg.Body)
	}
	if msg.BodyHTML != "<p>Hello World</p>" {
		t.Errorf("expected BodyHTML='<p>Hello World</p>', got %q", msg.BodyHTML)
	}
}

func TestGetMessageTextBody(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/me/messages/text1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":               "text1",
			"subject":          "Plain Email",
			"receivedDateTime": "2024-01-01T12:00:00Z",
			"bodyPreview":      "Just text",
			"isRead":           true,
			"from": map[string]any{
				"emailAddress": map[string]string{"address": "alice@example.com"},
			},
			"toRecipients": []map[string]any{
				{"emailAddress": map[string]string{"address": "bob@example.com"}},
			},
			"body": map[string]any{
				"contentType": "text",
				"content":     "Just plain text",
			},
		})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := newTestService(ts.URL)
	result, err := svc.Execute("get", map[string]string{"id": "text1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	msg := result.Data.(MessageDetail)
	if msg.Body != "Just plain text" {
		t.Errorf("expected Body='Just plain text', got %q", msg.Body)
	}
	if msg.BodyHTML != "" {
		t.Errorf("expected BodyHTML='', got %q", msg.BodyHTML)
	}
}
```

**Step 2: Update existing `TestGetMessage` to expect `BodyHTML`**

The existing `TestGetMessage` (line 108) sends a body without `contentType`. After the changes, this defaults to HTML treatment. Update the test fixture to include `"contentType": "text"` to preserve existing behavior, OR update the expected assertions. Since the existing test sends `"content": "Hello World"` (no HTML tags), both approaches work. Add `contentType` to make it explicit:

In the existing `TestGetMessage` fixture, change:

```go
"body": map[string]string{
	"content": "Hello World",
},
```

to:

```go
"body": map[string]any{
	"contentType": "text",
	"content":     "Hello World",
},
```

**Step 3: Run tests to verify new ones fail**

Run: `go test ./internal/service/outlook/ -run "TestGetMessageHTMLBody|TestGetMessageTextBody" -v`
Expected: FAIL -- `BodyHTML` field does not exist on `MessageDetail`

**Step 4: Commit**

```bash
git add internal/service/outlook/read_test.go
git commit -m "test: add Outlook tests for HTML body extraction and plain text handling"
```

---

### Task 6: Update Outlook structs and body handling -- implementation

**Files:**
- Modify: `internal/service/outlook/read.go:1-215`

**Step 1: Add `BodyHTML` field to `MessageDetail`**

In `internal/service/outlook/read.go`, update the `MessageDetail` struct (after the `Body` field at line 31):

```go
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
```

**Step 2: Add `ContentType` to `graphMessage.Body`**

Update the `Body` struct in `graphMessage` (lines 56-58):

```go
Body struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
} `json:"body"`
```

**Step 3: Add `htmlutil` import**

Add to the import block:

```go
"github.com/jhyoong/KumaApprove/internal/htmlutil"
```

Also add `"strings"` to the import block (needed for `strings.EqualFold`).

**Step 4: Update body population in `get()` method**

In the `get()` method (around lines 130-143), replace:

```go
return &service.Result{Data: MessageDetail{
	ID:      msg.ID,
	From:    msg.From.EmailAddress.Address,
	To:      to,
	Subject: msg.Subject,
	Date:    msg.ReceivedDateTime,
	Snippet: msg.BodyPreview,
	Body:    msg.Body.Content,
}}, nil
```

with:

```go
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
```

**Step 5: Run all Outlook tests**

Run: `go test ./internal/service/outlook/ -v`
Expected: all PASS (both old and new tests)

**Step 6: Commit**

```bash
git add internal/service/outlook/read.go
git commit -m "feat(outlook): extract plain text from HTML bodies, add bodyHtml field"
```

---

### Task 7: Full test suite and vet

**Files:** None (verification only)

**Step 1: Run full test suite**

Run: `go test ./...`
Expected: all PASS

**Step 2: Run go vet**

Run: `go vet ./...`
Expected: no issues

**Step 3: Run go build to verify compilation**

Run: `go build -o /dev/null ./cmd/kuma-approve/`
Expected: clean build, no errors

**Step 4: Final commit if any adjustments were needed**

If any fixes were required, commit them with an appropriate message.
