//go:build docker

package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const labToken = "ctf-proxy-real-world-lab-token"

type lab struct {
	t interface {
		Helper()
		Fatalf(string, ...any)
		Logf(string, ...any)
		Failed() bool
	}
	repo     string
	root     string
	stage    string
	binary   string
	control  int
	ports    map[string]int
	projects map[string]string
	proxies  map[string]string
	process  *exec.Cmd
	failed   bool
}

func newLab(t interface {
	Helper()
	Fatalf(string, ...any)
	Logf(string, ...any)
	Failed() bool
}) *lab {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	repo := filepath.Clean(filepath.Join(cwd, "../.."))
	root, err := os.MkdirTemp("", "ctf-proxy-real-world-lab-")
	if err != nil {
		t.Fatalf("create lab root: %v", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatalf("protect lab root: %v", err)
	}
	control, err := freePort()
	if err != nil {
		t.Fatalf("allocate control port: %v", err)
	}
	return &lab{t: t, repo: repo, root: root, stage: filepath.Join(root, "services"), binary: filepath.Join(root, "ctf-proxy"), control: control, ports: map[string]int{}, projects: map[string]string{}, proxies: map[string]string{}}
}

func (l *lab) setup() {
	l.t.Helper()
	if err := os.MkdirAll(l.stage, 0o700); err != nil {
		l.t.Fatalf("create stage: %v", err)
	}
	for _, name := range serviceNames() {
		port, err := freePort()
		if err != nil {
			l.t.Fatalf("allocate %s port: %v", name, err)
		}
		l.ports[name] = port
		source := filepath.Join(l.repo, "test", "lab", "services", name)
		target := filepath.Join(l.stage, name)
		if err := copyDir(source, target); err != nil {
			l.t.Fatalf("stage %s: %v", name, err)
		}
		if err := rewritePublishedPort(filepath.Join(target, "compose.yaml"), port); err != nil {
			l.t.Fatalf("rewrite %s port: %v", name, err)
		}
		project := fmt.Sprintf("ctf_proxy_lab_%d_%s", time.Now().UnixNano(), strings.ReplaceAll(name, "-", "_"))
		l.projects[name] = project
		if err := rewriteProjectName(filepath.Join(target, "compose.yaml"), project); err != nil {
			l.t.Fatalf("rewrite %s project name: %v", name, err)
		}
		l.run(l.repo, nil, "docker", "compose", "--file", filepath.Join(target, "compose.yaml"), "up", "--build", "--detach")
		l.waitPort(port)
	}
	l.run(l.repo, nil, pnpm(), "run", "build:frontend")
	l.run(l.repo, nil, "go", "build", "-tags", "production", "-o", l.binary, "./cmd/ctf-proxy")
	config := filepath.Join(l.root, "ctf-proxy.yaml")
	tokens := filepath.Join(l.root, ".tokens")
	configuration := fmt.Sprintf("version: 1\nmetrics:\n  competition_start: %q\n  round_duration: 2m\n  retention_rounds: 720\nfilter_files:\n  - %q\nproxies: []\n", time.Now().UTC().Format(time.RFC3339), filepath.Join(l.repo, "test", "lab", "filters.yaml"))
	if err := os.WriteFile(config, []byte(configuration), 0o600); err != nil {
		l.t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(tokens, []byte(labToken+"\n"), 0o600); err != nil {
		l.t.Fatalf("write token: %v", err)
	}
	logFile, err := os.OpenFile(filepath.Join(l.root, "ctf-proxy.stderr.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		l.t.Fatalf("open process log: %v", err)
	}
	l.process = exec.Command(l.binary)
	l.process.Dir = l.root
	l.process.Env = append(os.Environ(), "CTF_PROXY_CONFIG="+config, "CTF_PROXY_TOKENS_FILE="+tokens, fmt.Sprintf("CTF_PROXY_CONTROL_ADDR=127.0.0.1:%d", l.control), "CTF_PROXY_COMPOSE_ROOT="+l.stage)
	l.process.Stdout = logFile
	l.process.Stderr = logFile
	if err := l.process.Start(); err != nil {
		l.t.Fatalf("start ctf-proxy: %v", err)
	}
	l.waitHealth()
}

func (l *lab) cleanup() {
	if l.process != nil && l.process.Process != nil {
		_ = l.process.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() { done <- l.process.Wait() }()
		select {
		case <-time.After(5 * time.Second):
			_ = l.process.Process.Kill()
			<-done
		case <-done:
		}
	}
	for name := range l.projects {
		compose := filepath.Join(l.stage, name, "compose.yaml")
		output, _ := exec.Command("docker", "compose", "--file", compose, "logs", "--no-color").CombinedOutput()
		_ = os.WriteFile(filepath.Join(l.root, name+".docker.log"), output, 0o600)
		_ = exec.Command("docker", "compose", "--file", compose, "down", "--volumes", "--remove-orphans").Run()
	}
	if l.failed || l.t.Failed() || os.Getenv("CTF_PROXY_LAB_KEEP") == "1" {
		l.t.Logf("lab artifacts preserved at %s", l.root)
		return
	}
	_ = os.RemoveAll(l.root)
}

func (l *lab) run(dir string, env []string, name string, args ...string) {
	l.t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	command.Env = append(os.Environ(), env...)
	output, err := command.CombinedOutput()
	if err != nil {
		l.failed = true
		l.t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
}

func (l *lab) waitHealth() {
	l.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		request, _ := http.NewRequest(http.MethodGet, l.baseURL()+"/healthz", nil)
		request.Header.Set("Authorization", "Bearer "+labToken)
		response, err := http.DefaultClient.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	l.failed = true
	l.t.Fatalf("ctf-proxy control API did not become healthy")
}

func (l *lab) waitPort(port int) {
	l.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	address := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	l.failed = true
	l.t.Fatalf("service on %s did not become reachable", address)
}

func (l *lab) baseURL() string { return fmt.Sprintf("http://127.0.0.1:%d", l.control) }

func (l *lab) runPlaywright(spec string) error {
	env := []string{"LAB_BASE_URL=" + l.baseURL(), "LAB_CONTROL_TOKEN=" + labToken, "LAB_TCP_ECHO_PORT=" + fmt.Sprint(l.ports["tcp-echo"]), "LAB_TCP_ARCHIVE_PORT=" + fmt.Sprint(l.ports["tcp-archive"]), "LAB_HTTP_LOGIN_PORT=" + fmt.Sprint(l.ports["http-login"]), "LAB_HTTP_TEMPLATE_PORT=" + fmt.Sprint(l.ports["http-template"])}
	for name, proxy := range l.proxies {
		env = append(env, "LAB_"+strings.ToUpper(strings.ReplaceAll(name, "-", "_"))+"_PROXY="+proxy)
	}
	command := exec.Command(pnpm(), "exec", "playwright", "test", "--config", "test/lab/playwright/playwright.config.ts", "test/lab/playwright/"+spec)
	command.Dir = l.repo
	command.Env = append(os.Environ(), env...)
	output, err := command.CombinedOutput()
	if err != nil {
		l.failed = true
		return fmt.Errorf("playwright %s: %w\n%s", spec, err, output)
	}
	return nil
}

func (l *lab) proxiesFromAPI() {
	l.t.Helper()
	var result struct {
		Proxies []struct{ Name, Listen string } `json:"proxies"`
	}
	l.api(http.MethodGet, "/api/v1/proxies", nil, &result)
	for service, port := range l.ports {
		listen := fmt.Sprintf("0.0.0.0:%d", port)
		for _, proxy := range result.Proxies {
			if proxy.Listen == listen {
				l.proxies[service] = proxy.Name
			}
		}
		if l.proxies[service] == "" {
			l.t.Fatalf("no proxy found for %s on %s", service, listen)
		}
	}
}

func (l *lab) api(method, path string, body io.Reader, out any) {
	l.t.Helper()
	request, err := http.NewRequest(method, l.baseURL()+path, body)
	if err != nil {
		l.t.Fatalf("create API request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+labToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		l.t.Fatalf("call API %s: %v", path, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		l.t.Fatalf("API %s status = %d", path, response.StatusCode)
	}
	if out != nil && response.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(response.Body).Decode(out); err != nil {
			l.t.Fatalf("decode API %s: %v", path, err)
		}
	}
}

func (l *lab) events() []map[string]any {
	var result struct {
		Events []map[string]any `json:"events"`
	}
	l.api(http.MethodGet, "/api/v1/events?limit=100", nil, &result)
	return result.Events
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func rewritePublishedPort(path string, port int) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	matcher := regexp.MustCompile(`0\.0\.0\.0:\d+:`)
	updated := matcher.ReplaceAllString(string(b), fmt.Sprintf("0.0.0.0:%d:", port))
	if updated == string(b) {
		return errors.New("published port mapping not found")
	}
	return os.WriteFile(path, []byte(updated), 0o600)
}

func rewriteProjectName(path, project string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if strings.HasPrefix(string(b), "name:") {
		return errors.New("fixture unexpectedly defines a Compose project name")
	}
	return os.WriteFile(path, []byte("name: "+project+"\n"+string(b)), 0o600)
}

func copyDir(source, target string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return err
	}
	for _, entry := range entries {
		from, to := filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())
		if entry.IsDir() {
			if err := copyDir(from, to); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		if err := os.WriteFile(to, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func serviceNames() []string {
	return []string{"tcp-echo", "tcp-archive", "http-login", "http-template"}
}
func pnpm() string {
	if runtime := os.Getenv("PNPM_BINARY"); runtime != "" {
		return runtime
	}
	return "pnpm"
}

func stableMapJSON(value any) string {
	b, _ := json.Marshal(value)
	return string(bytes.TrimSpace(b))
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func withTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}
