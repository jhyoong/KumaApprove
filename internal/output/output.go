package output

import (
	"encoding/json"
	"fmt"
	"os"
)

type Envelope struct {
	Success bool      `json:"success"`
	Action  string    `json:"action"`
	Data    any       `json:"data"`
	Error   *ErrorInfo `json:"error"`
}

type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func Success(action string, data any) Envelope {
	return Envelope{
		Success: true,
		Action:  action,
		Data:    data,
	}
}

func Fail(action, code, message string) Envelope {
	return Envelope{
		Success: false,
		Action:  action,
		Error:   &ErrorInfo{Code: code, Message: message},
	}
}

func Print(env Envelope) {
	b, err := json.Marshal(env)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal output: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(b))
}

func PrintAndExit(env Envelope) {
	Print(env)
	if !env.Success {
		os.Exit(1)
	}
}
