package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry represents a single audit log record.
type Entry struct {
	Timestamp string            `json:"timestamp"`
	Action    string            `json:"action"`
	Account   string            `json:"account"`
	Params    map[string]string `json:"params,omitempty"`
	Status    string            `json:"status"`
	Result    string            `json:"result"`
	ErrorCode string            `json:"error_code,omitempty"`
}

// Logger writes append-only audit entries as newline-delimited JSON.
type Logger struct {
	path string
	mu   sync.Mutex
}

// NewLogger creates an audit logger that writes to the given file path.
// Parent directories are created if they do not exist.
func NewLogger(path string) (*Logger, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Logger{path: path}, nil
}

// Log writes an entry to the audit log file.
// The entry's Timestamp is set to the current UTC time in RFC 3339 format.
// Params are sanitised before writing.
func (l *Logger) Log(entry Entry) (err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry.Timestamp = time.Now().UTC().Format(time.RFC3339)
	entry.Params = sanitiseParams(entry.Params)

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	_, err = f.Write(data)
	return err
}

// sanitiseParams returns a copy of params with sensitive values truncated.
// The "body" key is truncated to 200 characters with "..." appended if it
// exceeds that length. Other keys are left unchanged.
func sanitiseParams(params map[string]string) map[string]string {
	if params == nil {
		return nil
	}

	out := make(map[string]string, len(params))
	for k, v := range params {
		if k == "body" {
			runes := []rune(v)
			if len(runes) > 200 {
				v = string(runes[:200]) + "..."
			}
		}
		out[k] = v
	}
	return out
}
