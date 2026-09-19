# HTML Email Body Extraction Design

## Problem

When emails are sent in HTML-only format (no `text/plain` MIME alternative), the tool fails to extract readable content:

- **Gmail**: `extractBody()` only looks for `text/plain` parts. HTML-only emails return an empty body. Additionally, the MIME part struct is flat and misses nested multipart structures.
- **Outlook**: The Graph API returns `body.content` as raw HTML by default. The body is returned with all HTML markup tags, making it unreadable.

## Approach

Shared `internal/htmlutil` package with an HTML-to-text function, used by both Gmail and Outlook services. Add a `bodyHtml` field to both services' `MessageDetail` struct so consumers can access both formats.

## Design

### 1. New `internal/htmlutil` package

Single exported function:

```go
func StripTags(html string) string
```

Behavior:
- Strips HTML tags, decodes HTML entities
- Preserves line breaks from block elements (p, br, div, li, tr)
- Collapses whitespace
- Excludes content from style/script tags
- Best-effort on malformed HTML (no errors returned)
- Uses `golang.org/x/net/html` tokenizer (already an indirect dependency)

### 2. Gmail changes (`internal/service/gmail/read.go`)

**Struct changes:**

Replace the inline `Parts` definition in `gmailMessage.Payload` with a recursive struct:

```go
type gmailPart struct {
    MimeType string     `json:"mimeType"`
    Body     struct {
        Data string `json:"data"`
    } `json:"body"`
    Parts []gmailPart `json:"parts"`
}
```

**Output changes:**

Add `BodyHTML` to `MessageDetail`:

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

**Extraction logic:**

Rename `extractBody` to `extractBodies`, returning `(plainText, html string)`:

1. Recursively walk all parts to find `text/plain` and `text/html` content
2. If `text/plain` found, use it for `Body`
3. Always populate `BodyHTML` from `text/html` if found (or from payload body)
4. If no `text/plain` but HTML exists, derive `Body` via `htmlutil.StripTags(html)`

### 3. Outlook changes (`internal/service/outlook/read.go`)

**Struct changes:**

Add `ContentType` to the existing `Body` struct in `graphMessage`:

```go
Body struct {
    ContentType string `json:"contentType"`
    Content     string `json:"content"`
} `json:"body"`
```

**Output changes:**

Add `BodyHTML` to `MessageDetail` (same pattern as Gmail).

**Body population logic in `get()`:**

- If `ContentType == "html"`: `BodyHTML = content`, `Body = htmlutil.StripTags(content)`
- If `ContentType == "text"`: `Body = content`, `BodyHTML` empty
- Default (no content type): treat as HTML

### 4. Testing

**`internal/htmlutil`:**
- Basic tags, nested tags, HTML entities
- Block elements producing newlines
- Empty/whitespace-only input, already-plain text
- Style/script tag content excluded

**Gmail:**
- HTML-only simple message (payload body is HTML, no parts)
- HTML-only multipart message (parts contain only `text/html`)
- Nested multipart structure (`multipart/mixed` > `multipart/alternative` > `text/html`)
- Both `text/plain` and `text/html` present -- both fields populated
- Existing tests continue to pass

**Outlook:**
- HTML body content -- `Body` is stripped text, `BodyHTML` is raw HTML
- Plain text body content -- `Body` is text, `BodyHTML` is empty
- Existing tests updated for `BodyHTML` field

### 5. Error handling and edge cases

- HTML parsing errors: best-effort, return whatever text was extracted
- No body: both fields empty (existing behavior)
- Empty HTML tags only: `Body` empty, `BodyHTML` has raw HTML
- Large HTML: no truncation in `StripTags` (truncation handled by audit log and Telegram approval message formatting)

## Files changed

- `internal/htmlutil/strip.go` (new)
- `internal/htmlutil/strip_test.go` (new)
- `internal/service/gmail/read.go` (modified)
- `internal/service/gmail/read_test.go` (modified)
- `internal/service/outlook/read.go` (modified)
- `internal/service/outlook/read_test.go` (modified)
