package credstore

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func GetMachineID() (string, error) {
	switch runtime.GOOS {
	case "linux":
		return getLinuxMachineID()
	case "darwin":
		return getDarwinMachineID()
	default:
		return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func getLinuxMachineID() (string, error) {
	data, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		return "", fmt.Errorf("reading /etc/machine-id: %w", err)
	}
	id := strings.TrimSpace(string(data))
	if len(id) == 0 {
		return "", fmt.Errorf("/etc/machine-id is empty")
	}
	return id, nil
}

func getDarwinMachineID() (string, error) {
	out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return "", fmt.Errorf("running ioreg: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "IOPlatformUUID") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				uuid := strings.TrimSpace(parts[1])
				uuid = strings.Trim(uuid, `"`)
				if len(uuid) > 0 {
					return uuid, nil
				}
			}
		}
	}
	return "", fmt.Errorf("IOPlatformUUID not found in ioreg output")
}
