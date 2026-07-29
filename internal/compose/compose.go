// Package compose implements the deliberately narrow Docker Compose scan and
// configuration workflow
// workflow used by Attack & Defense CTF vulnboxes.
package compose

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"
)

const (
	ComposeFileName = "docker-compose.yaml"
	stateDirectory  = ".ctf-proxy-state"
)

// DefaultFileNames covers the Compose file spellings used by Docker Compose.
var DefaultFileNames = []string{"docker-compose.yaml", "docker-compose.yml", "compose.yaml", "compose.yml"}

// Candidate is a safe, reviewable published TCP mapping.
type Candidate struct {
	ID          string `json:"id"`
	Project     string `json:"project"`
	ComposeFile string `json:"compose_file"`
	Service     string `json:"service"`
	Listen      string `json:"listen"`
	Target      string `json:"target"`
	Upstream    string `json:"upstream"`
	Eligible    bool   `json:"eligible"`
	Reason      string `json:"reason,omitempty"`
}

// Project groups candidates found in a single Compose project.
type Project struct {
	Name        string      `json:"name"`
	ComposeFile string      `json:"compose_file"`
	Candidates  []Candidate `json:"candidates"`
}

// Selection is one dashboard-approved candidate and proxy type.
type Selection struct {
	ID       string `json:"id"`
	Protocol string `json:"protocol"`
	Scheme   string `json:"scheme,omitempty"`
}

// Deployment is safe persisted state exposed to the dashboard.
type Deployment struct {
	ID          string `json:"id"`
	Project     string `json:"project"`
	ComposeFile string `json:"compose_file"`
	Service     string `json:"service"`
	Listen      string `json:"listen"`
	Upstream    string `json:"upstream"`
	Proxy       string `json:"proxy"`
	Protocol    string `json:"protocol"`
	State       string `json:"state"`
}

type Entry struct {
	Deployment
	Index int `json:"index"`
}

type Record struct {
	ProjectPath string  `json:"project_path"`
	ComposePath string  `json:"compose_path"`
	Original    []byte  `json:"original"`
	ExpectedSHA string  `json:"expected_sha"`
	Entries     []Entry `json:"entries"`
}

// Discover returns immediate Compose projects under root. It deliberately
// avoids recursive traversal and symlinks so a scan cannot wander outside the
// CTF service directory.
func Discover(root string) ([]Project, error) {
	return DiscoverWithNames(root, DefaultFileNames)
}

// DiscoverWithNames scans each immediate service directory for the supplied
// safe Compose filenames. A directory may contain more than one Compose file;
// each is listed separately so the operator can choose the authoritative one.
func DiscoverWithNames(root string, names []string) ([]Project, error) {
	names = validFileNames(names)
	if len(names) == 0 {
		return nil, errors.New("no valid Compose filenames configured")
	}
	dirs, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read compose root: %w", err)
	}
	projects := make([]Project, 0)
	reservedPorts := make(map[int]struct{})
	for _, dir := range dirs {
		if !dir.IsDir() || dir.Type()&os.ModeSymlink != 0 {
			continue
		}
		for _, name := range names {
			path := filepath.Join(root, dir.Name(), name)
			info, err := os.Lstat(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			project, err := discoverFile(path, reservedPorts)
			if err != nil {
				projects = append(projects, Project{Name: dir.Name() + " (" + name + ")", ComposeFile: path, Candidates: []Candidate{{ID: id(path, "", -1), Project: dir.Name(), ComposeFile: path, Eligible: false, Reason: "Compose file could not be parsed safely"}}})
				continue
			}
			if len(names) > 1 {
				project.Name += " (" + name + ")"
				for index := range project.Candidates {
					project.Candidates[index].Project = project.Name
				}
			}
			projects = append(projects, project)
		}
	}
	return projects, nil
}

func validFileNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	valid := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || filepath.Base(name) != name || name == "." || name == ".." {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		valid = append(valid, name)
	}
	return valid
}

func discoverFile(path string, reservedPorts map[int]struct{}) (Project, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Project{}, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return Project{}, err
	}
	root := documentRoot(&doc)
	if root == nil || root.Kind != yaml.MappingNode {
		return Project{}, errors.New("root is not a mapping")
	}
	services := mapValue(root, "services")
	if services == nil || services.Kind != yaml.MappingNode {
		return Project{}, errors.New("services is not a mapping")
	}
	project := Project{Name: filepath.Base(filepath.Dir(path)), ComposeFile: path}
	for i := 0; i+1 < len(services.Content); i += 2 {
		service := services.Content[i].Value
		definition := services.Content[i+1]
		if definition.Kind != yaml.MappingNode {
			continue
		}
		ports := mapValue(definition, "ports")
		if ports == nil || ports.Kind != yaml.SequenceNode {
			continue
		}
		hostNetwork := mapValue(definition, "network_mode")
		for index, port := range ports.Content {
			candidate := Candidate{ID: id(path, service, index), Project: project.Name, ComposeFile: path, Service: service}
			if hostNetwork != nil && strings.EqualFold(hostNetwork.Value, "host") {
				candidate.Reason = "host-network services are unsupported"
				project.Candidates = append(project.Candidates, candidate)
				continue
			}
			listen, target, reason := parsePort(port)
			if reason != "" {
				candidate.Reason = reason
				project.Candidates = append(project.Candidates, candidate)
				continue
			}
			candidate.Listen, candidate.Target = listen, target
			if !publicListen(listen) {
				candidate.Reason = "loopback-only port mapping"
				project.Candidates = append(project.Candidates, candidate)
				continue
			}
			private, err := freePort(reservedPorts)
			if err != nil {
				return Project{}, err
			}
			candidate.Upstream = net.JoinHostPort("127.0.0.1", strconv.Itoa(private))
			reservedPorts[private] = struct{}{}
			candidate.Eligible = true
			project.Candidates = append(project.Candidates, candidate)
		}
	}
	return project, nil
}

func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		return doc.Content[0]
	}
	return doc
}
func mapValue(n *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}
func setMapValue(n *yaml.Node, key, value string) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			n.Content[i+1].Value = value
			n.Content[i+1].Tag = "!!str"
			return
		}
	}
	n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

func parsePort(node *yaml.Node) (string, string, string) {
	if node.Kind == yaml.ScalarNode {
		value := strings.TrimSuffix(node.Value, "/tcp")
		if strings.Contains(value, "/") {
			return "", "", "only TCP port mappings are supported"
		}
		parts := strings.Split(value, ":")
		if len(parts) != 2 && len(parts) != 3 {
			return "", "", "only explicit single host ports are supported"
		}
		host, published, target := "", parts[0], parts[1]
		if len(parts) == 3 {
			host, published, target = parts[0], parts[1], parts[2]
		}
		if strings.Contains(host, "[") || strings.Contains(host, ":") {
			return "", "", "IPv6 short-form mappings are unsupported"
		}
		if !singlePort(published) || !singlePort(target) {
			return "", "", "only numeric single ports are supported"
		}
		return net.JoinHostPort(host, published), target, ""
	}
	if node.Kind != yaml.MappingNode {
		return "", "", "unsupported ports entry"
	}
	protocol := mapValue(node, "protocol")
	if protocol != nil && protocol.Value != "" && strings.ToLower(protocol.Value) != "tcp" {
		return "", "", "only TCP port mappings are supported"
	}
	published, target := mapValue(node, "published"), mapValue(node, "target")
	if published == nil || target == nil || !singlePort(published.Value) || !singlePort(target.Value) {
		return "", "", "only explicit numeric published and target ports are supported"
	}
	host := mapValue(node, "host_ip")
	value := ""
	if host != nil {
		value = host.Value
	}
	return net.JoinHostPort(value, published.Value), target.Value, ""
}

func singlePort(value string) bool {
	n, err := strconv.Atoi(value)
	return err == nil && n > 0 && n <= 65535
}
func publicListen(value string) bool {
	host, _, err := net.SplitHostPort(value)
	if err != nil {
		return false
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		return true
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}
func freePort(reserved map[int]struct{}) (int, error) {
	for p := 20000; p <= 59999; p++ {
		if _, exists := reserved[p]; exists {
			continue
		}
		l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)))
		if err == nil {
			_ = l.Close()
			return p, nil
		}
	}
	return 0, errors.New("no loopback port available")
}
func id(path, service string, index int) string {
	sum := sha256.Sum256([]byte(path + "\x00" + service + "\x00" + strconv.Itoa(index)))
	return hex.EncodeToString(sum[:8])
}
func hash(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

// Rewrite applies selected candidate entries to a Compose document.
func Rewrite(source []byte, selected map[string]Candidate) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(source, &doc); err != nil {
		return nil, err
	}
	root := documentRoot(&doc)
	services := mapValue(root, "services")
	if services == nil {
		return nil, errors.New("services is missing")
	}
	// IDs include absolute paths, so locate selected entries by service and index
	// encoded in Candidate metadata rather than trusting YAML content.
	for _, candidate := range selected {
		for i := 0; i+1 < len(services.Content); i += 2 {
			if services.Content[i].Value != candidate.Service {
				continue
			}
			ports := mapValue(services.Content[i+1], "ports")
			if ports == nil {
				continue
			}
			for index, node := range ports.Content {
				if id(candidate.ComposeFile, candidate.Service, index) != candidate.ID {
					continue
				}
				_, target, reason := parsePort(node)
				if reason != "" || target != candidate.Target {
					return nil, errors.New("Compose mapping changed since discovery")
				}
				privatePort := strings.TrimPrefix(candidate.Upstream, "127.0.0.1:")
				if node.Kind == yaml.ScalarNode {
					node.Value = "127.0.0.1:" + privatePort + ":" + target
				} else {
					setMapValue(node, "host_ip", "127.0.0.1")
					setMapValue(node, "published", privatePort)
				}
			}
		}
	}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&doc); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Store owns private restore records. It is intentionally filesystem-backed,
// separate from the operator-visible proxy configuration.
type Store struct{ dir string }

func NewStore(configPath string) *Store {
	return &Store{dir: filepath.Join(filepath.Dir(configPath), stateDirectory)}
}
func (s *Store) Save(record Record) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	path := s.path(record.ComposePath)
	temp, err := os.CreateTemp(s.dir, ".state-*")
	if err != nil {
		return err
	}
	if err := temp.Chmod(0o600); err == nil {
		_, err = temp.Write(data)
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(temp.Name())
		return err
	}
	return os.Rename(temp.Name(), path)
}
func (s *Store) path(composePath string) string {
	return filepath.Join(s.dir, hash([]byte(composePath))+".json")
}
func (s *Store) Remove(composePath string) error {
	err := os.Remove(s.path(composePath))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func (s *Store) Records() ([]Record, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	records := make([]Record, 0)
	for _, item := range entries {
		if item.IsDir() || filepath.Ext(item.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, item.Name()))
		if err != nil {
			return nil, err
		}
		var r Record
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ComposePath < records[j].ComposePath })
	return records, nil
}

// RunCompose is replaceable in tests. It returns only a safe generic error.
var RunCompose = func(ctx context.Context, dir string, args ...string) error {
	command := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		_ = output
		return errors.New("docker compose command failed")
	}
	return nil
}

func CheckCompose(ctx context.Context) error { return RunCompose(ctx, "", "version") }
func Recreate(ctx context.Context, projectPath, composePath string, services []string) error {
	if err := RunCompose(ctx, projectPath, "-f", composePath, "config", "-q"); err != nil {
		return err
	}
	args := append([]string{"-f", composePath, "up", "-d", "--no-deps", "--force-recreate"}, services...)
	return RunCompose(ctx, projectPath, args...)
}
func Context() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 45*time.Second)
}

// WriteAtomic preserves restrictive mode and avoids torn Compose files.
func WriteAtomic(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".ctf-proxy-compose-*")
	if err != nil {
		return err
	}
	if err = temp.Chmod(info.Mode().Perm()); err == nil {
		_, err = temp.Write(data)
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(temp.Name())
		return err
	}
	return os.Rename(temp.Name(), path)
}
