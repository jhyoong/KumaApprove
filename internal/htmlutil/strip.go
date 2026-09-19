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
