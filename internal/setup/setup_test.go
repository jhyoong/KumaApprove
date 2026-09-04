package setup

import (
	"bufio"
	"strings"
	"testing"
)

func TestReadLine(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("hello\n"))
	got := readLine(reader, "prompt: ")
	if got != "hello" {
		t.Errorf("readLine() = %q, want %q", got, "hello")
	}
}

func TestReadLineTrimsWhitespace(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("  hello  \n"))
	got := readLine(reader, "prompt: ")
	if got != "hello" {
		t.Errorf("readLine() = %q, want %q", got, "hello")
	}
}
