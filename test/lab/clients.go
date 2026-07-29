//go:build docker

package lab

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
)

type clientResult struct {
	OK              bool `json:"ok"`
	Status          int  `json:"status"`
	Echoed          bool `json:"echoed"`
	FlagFound       bool `json:"flag_found"`
	FlagFormatValid bool `json:"flag_format_valid"`
}

func (l *lab) client(service string, args ...string) clientResult {
	l.t.Helper()
	commandArgs := append([]string{filepath.Join(l.repo, "test", "lab", "services", service, "client.py"), "--host", "127.0.0.1", "--port", fmt.Sprint(l.ports[service])}, args...)
	output, err := exec.Command("python3", commandArgs...).Output()
	if err != nil {
		l.t.Fatalf("run %s client: %v", service, err)
	}
	var result clientResult
	if err := json.Unmarshal(output, &result); err != nil {
		l.t.Fatalf("decode %s client output: %v", service, err)
	}
	return result
}
