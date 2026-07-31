// Command lab starts a disposable, interactive real-world CTF proxy lab.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	mathrand "math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

var services = []string{"tcp-echo", "tcp-archive", "http-login", "http-template"}

type trafficMode string

const (
	trafficNone  trafficMode = "none"
	trafficMixed trafficMode = "mixed"
)

type environment struct {
	repo        string
	root        string
	stage       string
	binary      string
	token       string
	control     int
	ports       map[string]int
	projects    map[string]string
	proxy       *exec.Cmd
	proxyDone   chan error
	proxyExited bool
	trafficStop context.CancelFunc
	trafficDone chan struct{}
	trafficWG   sync.WaitGroup
}

func main() {
	traffic := flag.String("traffic", string(trafficNone), "background traffic mode: none or mixed")
	trafficInterval := flag.Duration("traffic-interval", 200*time.Millisecond, "interval between background traffic rounds")
	flag.Parse()
	mode := trafficMode(*traffic)
	if mode != trafficNone && mode != trafficMixed {
		fmt.Fprintln(os.Stderr, "ctf-proxy lab: --traffic must be none or mixed")
		os.Exit(2)
	}
	if *trafficInterval <= 0 {
		fmt.Fprintln(os.Stderr, "ctf-proxy lab: --traffic-interval must be greater than zero")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, mode, *trafficInterval); err != nil {
		fmt.Fprintln(os.Stderr, "ctf-proxy lab:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, traffic trafficMode, trafficInterval time.Duration) (result error) {
	if err := preflight(); err != nil {
		return err
	}
	repo, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	root, err := os.MkdirTemp("", "ctf-proxy-interactive-lab-")
	if err != nil {
		return fmt.Errorf("create lab directory: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return fmt.Errorf("protect lab directory: %w", err)
	}
	control, err := freePort()
	if err != nil {
		return fmt.Errorf("allocate control port: %w", err)
	}
	token, err := randomToken()
	if err != nil {
		return fmt.Errorf("generate control token: %w", err)
	}
	env := &environment{repo: repo, root: root, stage: filepath.Join(root, "services"), binary: filepath.Join(root, "ctf-proxy"), token: token, control: control, ports: map[string]int{}, projects: map[string]string{}}
	defer func() {
		env.cleanup()
		if result == nil && os.Getenv("CTF_PROXY_LAB_KEEP") != "1" {
			_ = os.RemoveAll(root)
		} else {
			fmt.Fprintf(os.Stderr, "Lab artifacts preserved at %s\n", root)
		}
		fmt.Fprintln(os.Stderr, "Interactive lab shutdown complete.")
	}()
	if err := env.start(); err != nil {
		return err
	}
	env.printInstructions()
	env.startTraffic(ctx, traffic, trafficInterval)

	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "Shutting down ctf-proxy and all disposable lab services…")
		return nil
	case err := <-env.proxyDone:
		env.proxyExited = true
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return fmt.Errorf("ctf-proxy stopped unexpectedly: %w", err)
		}
		return errors.New("ctf-proxy stopped unexpectedly")
	}
}

func preflight() error {
	for _, command := range [][]string{{"docker", "compose", "version"}, {"python3", "--version"}, {"pnpm", "--version"}} {
		if output, err := exec.Command(command[0], command[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("requires %s: %w (%s)", strings.Join(command, " "), err, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

func (e *environment) start() error {
	if err := os.MkdirAll(e.stage, 0o700); err != nil {
		return err
	}
	for _, service := range services {
		port, err := freePort()
		if err != nil {
			return err
		}
		e.ports[service] = port
		target := filepath.Join(e.stage, service)
		if err := copyDir(filepath.Join(e.repo, "test", "lab", "services", service), target); err != nil {
			return fmt.Errorf("stage %s: %w", service, err)
		}
		compose := filepath.Join(target, "compose.yaml")
		if err := rewritePort(compose, port); err != nil {
			return fmt.Errorf("stage %s port: %w", service, err)
		}
		project := fmt.Sprintf("ctf_proxy_interactive_%d_%s", time.Now().UnixNano(), strings.ReplaceAll(service, "-", "_"))
		e.projects[service] = project
		if err := rewriteProject(compose, project); err != nil {
			return fmt.Errorf("stage %s project: %w", service, err)
		}
		if err := command(e.repo, nil, "docker", "compose", "--file", compose, "up", "--build", "--detach"); err != nil {
			return err
		}
		if err := waitPort(port); err != nil {
			return fmt.Errorf("wait for %s: %w", service, err)
		}
	}
	if err := command(e.repo, nil, "pnpm", "run", "build:frontend"); err != nil {
		return err
	}
	if err := command(e.repo, nil, "go", "build", "-tags", "production", "-o", e.binary, "./cmd/ctf-proxy"); err != nil {
		return err
	}
	config, tokens := filepath.Join(e.root, "ctf-proxy.yaml"), filepath.Join(e.root, ".tokens")
	configuration := fmt.Sprintf("version: 1\nmetrics:\n  competition_start: %q\n  round_duration: 2m\n  retention_rounds: 720\nfilter_files:\n  - %q\nproxies: []\n", time.Now().UTC().Format(time.RFC3339), filepath.Join(e.repo, "test", "lab", "filters.yaml"))
	if err := os.WriteFile(config, []byte(configuration), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(tokens, []byte(e.token+"\n"), 0o600); err != nil {
		return err
	}
	e.proxy = exec.Command(e.binary)
	e.proxy.Dir = e.root
	e.proxy.Env = append(os.Environ(), "CTF_PROXY_CONFIG="+config, "CTF_PROXY_TOKENS_FILE="+tokens, fmt.Sprintf("CTF_PROXY_CONTROL_ADDR=127.0.0.1:%d", e.control), "CTF_PROXY_COMPOSE_ROOT="+e.stage)
	e.proxy.Stdout, e.proxy.Stderr = os.Stdout, os.Stderr
	if err := e.proxy.Start(); err != nil {
		return err
	}
	e.proxyDone = make(chan error, 1)
	go func() { e.proxyDone <- e.proxy.Wait() }()
	return e.waitHealth()
}

func (e *environment) cleanup() {
	if e.trafficStop != nil {
		e.trafficStop()
		<-e.trafficDone
		e.trafficWG.Wait()
	}
	if e.proxy != nil && e.proxy.Process != nil && !e.proxyExited {
		_ = e.proxy.Process.Signal(os.Interrupt)
		select {
		case <-e.proxyDone:
			e.proxyExited = true
		case <-time.After(5 * time.Second):
			_ = e.proxy.Process.Kill()
			<-e.proxyDone
			e.proxyExited = true
		}
	}
	for service := range e.projects {
		compose := filepath.Join(e.stage, service, "compose.yaml")
		_ = command(e.repo, nil, "docker", "compose", "--file", compose, "down", "--volumes", "--remove-orphans")
	}
}

func (e *environment) waitHealth() error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		request, _ := http.NewRequest(http.MethodGet, e.baseURL()+"/healthz", nil)
		request.Header.Set("Authorization", "Bearer "+e.token)
		response, err := http.DefaultClient.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("control API did not become healthy")
}

func (e *environment) baseURL() string { return fmt.Sprintf("http://127.0.0.1:%d", e.control) }

func (e *environment) printInstructions() {
	fmt.Printf("\nInteractive ctf-proxy lab is running.\n\nDashboard: %s\nControl token: %s\nLab directory: %s\n\n", e.baseURL(), e.token, e.root)
	fmt.Println("Initially each fixture is directly exposed. In the dashboard, open Proxies, choose Scan and configure, select all four services, then Apply.")
	fmt.Println("After takeover, the same ports are mediated by ctf-proxy. Add filters in the Filters view and watch Events in the dashboard.")
	fmt.Println("\nFixture endpoints and example clients:")
	fmt.Printf("  TCP echo:      127.0.0.1:%d  python3 test/lab/services/tcp-echo/client.py --host 127.0.0.1 --port %d --admin\n", e.ports["tcp-echo"], e.ports["tcp-echo"])
	fmt.Printf("  TCP archive:   127.0.0.1:%d  python3 test/lab/services/tcp-archive/client.py --host 127.0.0.1 --port %d --exploit\n", e.ports["tcp-archive"], e.ports["tcp-archive"])
	fmt.Printf("  HTTP login:    127.0.0.1:%d  python3 test/lab/services/http-login/client.py --host 127.0.0.1 --port %d --admin-exploit\n", e.ports["http-login"], e.ports["http-login"])
	fmt.Printf("  HTTP template: 127.0.0.1:%d  python3 test/lab/services/http-template/client.py --host 127.0.0.1 --port %d --exploit\n", e.ports["http-template"], e.ports["http-template"])
	fmt.Println("\nPress Ctrl-C to stop the proxy and remove the disposable Docker projects.")
}

// startTraffic repeatedly runs the existing Python client CLIs. Errors are
// intentionally ignored because listener recreation during takeover and
// restoration briefly interrupts individual exchanges.
func (e *environment) startTraffic(ctx context.Context, mode trafficMode, interval time.Duration) {
	if mode == trafficNone {
		return
	}
	fmt.Printf("\nBackground mixed traffic started (one request to every service every %s; 80%% benign, 20%% exploit per request).\n", interval)
	trafficContext, stop := context.WithCancel(ctx)
	e.trafficStop = stop
	e.trafficDone = make(chan struct{})
	slots := make(chan struct{}, 2)
	go func() {
		defer close(e.trafficDone)
		launch := func() {
			select {
			case slots <- struct{}{}:
				e.trafficWG.Add(1)
				go func() {
					defer e.trafficWG.Done()
					defer func() { <-slots }()
					e.runTrafficCycle(trafficContext, mode)
				}()
			default:
				// Keep the requested cadence without allowing delayed clients to pile up.
			}
		}
		launch()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-trafficContext.Done():
				return
			case <-ticker.C:
				launch()
			}
		}
	}()
}

func (e *environment) runTrafficCycle(ctx context.Context, mode trafficMode) {
	if mode != trafficMixed {
		return
	}
	requests := trafficRequests()
	var clients sync.WaitGroup
	clients.Add(len(requests))
	for _, request := range requests {
		go func(request trafficRequest) {
			defer clients.Done()
			e.runClient(ctx, request.service, request.args...)
		}(request)
	}
	clients.Wait()
}

type trafficRequest struct {
	service string
	args    []string
}

// trafficRequests selects one request for each fixture service. Each request
// independently has an 80% chance of being benign and a 20% chance of being an
// exploit, so a traffic round exercises every service concurrently.
func trafficRequests() []trafficRequest {
	benign := []trafficRequest{
		{service: "tcp-echo", args: []string{"--message", "lab-heartbeat"}},
		{service: "tcp-archive"},
		{service: "http-login", args: []string{"--username", "alice", "--password", "wonderland"}},
		{service: "http-template"},
	}
	malicious := []trafficRequest{
		{service: "tcp-echo", args: []string{"--admin"}},
		{service: "tcp-archive", args: []string{"--exploit"}},
		{service: "http-login", args: []string{"--admin-exploit"}},
		{service: "http-template", args: []string{"--exploit"}},
	}
	requests := make([]trafficRequest, len(benign))
	for index := range benign {
		requests[index] = benign[index]
		if mathrand.IntN(5) == 0 {
			requests[index] = malicious[index]
		}
	}
	return requests
}

func (e *environment) runClient(ctx context.Context, service string, args ...string) {
	commandArgs := append([]string{filepath.Join(e.repo, "test", "lab", "services", service, "client.py"), "--host", "127.0.0.1", "--port", fmt.Sprint(e.ports[service])}, args...)
	command := exec.CommandContext(ctx, "python3", commandArgs...)
	command.Dir = e.repo
	_ = command.Run()
}

func command(dir string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir, cmd.Env = dir, append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, output)
	}
	return nil
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func waitPort(port int) error {
	deadline := time.Now().Add(30 * time.Second)
	address := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("%s did not become reachable", address)
}

func randomToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func rewritePort(path string, port int) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated := regexp.MustCompile(`0\.0\.0\.0:\d+:`).ReplaceAllString(string(b), fmt.Sprintf("0.0.0.0:%d:", port))
	if updated == string(b) {
		return errors.New("published port mapping not found")
	}
	return os.WriteFile(path, []byte(updated), 0o600)
}

func rewriteProject(path, project string) error {
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
