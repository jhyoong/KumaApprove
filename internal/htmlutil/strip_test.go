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
