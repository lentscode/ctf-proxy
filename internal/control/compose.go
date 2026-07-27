package control

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/lentscode/ctf-proxy/internal/compose"
	"github.com/lentscode/ctf-proxy/internal/config"
)

// ComposeManager coordinates the CTF-only Compose workflow with proxy lifecycle.
type ComposeManager struct {
	mu        sync.Mutex
	root      string
	fileNames []string
	proxies   *Manager
	store     *compose.Store
	projects  []compose.Project
	revision  string
	hashes    map[string]string
}

func NewComposeManager(root, configPath string, proxies *Manager, fileNames ...[]string) *ComposeManager {
	names := compose.DefaultFileNames
	if len(fileNames) > 0 && len(fileNames[0]) > 0 {
		names = fileNames[0]
	}
	return &ComposeManager{root: root, fileNames: names, proxies: proxies, store: compose.NewStore(configPath)}
}

func (m *ComposeManager) Discover() ([]compose.Project, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	projects, err := compose.DiscoverWithNames(m.root, m.fileNames)
	if err != nil {
		return nil, "", err
	}
	hashes := make(map[string]string)
	for _, project := range projects {
		if source, readErr := osReadFile(project.ComposeFile); readErr == nil {
			hashes[project.ComposeFile] = sha(source)
		}
	}
	m.projects = projects
	m.hashes = hashes
	m.revision = revision(projects)
	return projects, m.revision, nil
}

func (m *ComposeManager) Deployments() ([]compose.Deployment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deploymentsLocked()
}
func (m *ComposeManager) deploymentsLocked() ([]compose.Deployment, error) {
	records, err := m.store.Records()
	if err != nil {
		return nil, err
	}
	result := make([]compose.Deployment, 0)
	for _, record := range records {
		current, err := osReadFile(record.ComposePath)
		drifted := err != nil || sha(current) != record.ExpectedSHA
		for _, entry := range record.Entries {
			d := entry.Deployment
			if drifted {
				d.State = "drifted"
			}
			result = append(result, d)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// Apply edits Compose files, recreates selected services, and starts owned proxies.
func (m *ComposeManager) Apply(rev string, selections []compose.Selection) ([]compose.Deployment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rev == "" || rev != m.revision {
		return nil, errors.New("discovery is stale; scan again")
	}
	if len(selections) == 0 {
		return nil, errors.New("select at least one published port")
	}
	byID := make(map[string]compose.Candidate)
	for _, p := range m.projects {
		for _, c := range p.Candidates {
			byID[c.ID] = c
		}
	}
	chosen := make(map[string]compose.Selection)
	for _, selection := range selections {
		if _, exists := chosen[selection.ID]; exists {
			return nil, errors.New("duplicate mapping selection")
		}
		candidate, ok := byID[selection.ID]
		if !ok || !candidate.Eligible {
			return nil, errors.New("selected mapping is no longer eligible")
		}
		if selection.Protocol != "tcp" && selection.Protocol != "http" {
			return nil, errors.New("protocol must be tcp or http")
		}
		if selection.Protocol == "http" && selection.Scheme != "http" && selection.Scheme != "https" {
			return nil, errors.New("HTTP mappings need an http or https scheme")
		}
		chosen[selection.ID] = selection
	}
	if records, err := m.store.Records(); err != nil {
		return nil, err
	} else {
		for _, record := range records {
			for _, entry := range record.Entries {
				if _, ok := chosen[entry.ID]; ok {
					return nil, errors.New("selected mapping is already managed")
				}
			}
		}
	}
	ctx, cancel := compose.Context()
	defer cancel()
	if err := compose.CheckCompose(ctx); err != nil {
		return nil, errors.New("docker compose v2 is unavailable")
	}
	groups := make(map[string][]compose.Candidate)
	for id := range chosen {
		c := byID[id]
		groups[c.ComposeFile] = append(groups[c.ComposeFile], c)
	}
	type applied struct {
		record   compose.Record
		original []byte
		services []string
		proxies  []string
	}
	appliedGroups := make([]applied, 0, len(groups))
	success := false
	defer func() {
		if success {
			return
		}
		// A later project failing must not leave earlier projects partially
		// taken over. This is best-effort because Docker itself has no
		// cross-project transaction, but the saved originals make recovery
		// deterministic on the next operator action.
		for i := len(appliedGroups) - 1; i >= 0; i-- {
			current := appliedGroups[i]
			for _, name := range current.proxies {
				_ = m.proxies.Delete(name)
			}
			_ = compose.WriteAtomic(current.record.ComposePath, current.original)
			_ = compose.Recreate(ctx, current.record.ProjectPath, current.record.ComposePath, current.services)
			_ = m.store.Remove(current.record.ComposePath)
		}
	}()
	for path, candidates := range groups {
		original, err := osReadFile(path)
		if err != nil {
			return nil, err
		}
		if m.hashes[path] == "" || sha(original) != m.hashes[path] {
			return nil, errors.New("Compose file changed since discovery; scan again")
		}
		selected := make(map[string]compose.Candidate)
		for _, c := range candidates {
			selected[c.ID] = c
		}
		rewritten, err := compose.Rewrite(original, selected)
		if err != nil {
			return nil, err
		}
		entries := make([]compose.Entry, 0, len(candidates))
		services := make([]string, 0, len(candidates))
		for _, c := range candidates {
			choice := chosen[c.ID]
			name := proxyName(c)
			if _, err := m.proxies.Get(name); err == nil {
				return nil, fmt.Errorf("proxy %q already exists", name)
			}
			upstream := c.Upstream
			if choice.Protocol == "http" {
				upstream = choice.Scheme + "://" + c.Upstream
			}
			entries = append(entries, compose.Entry{Deployment: compose.Deployment{ID: c.ID, Project: c.Project, ComposeFile: c.ComposeFile, Service: c.Service, Listen: c.Listen, Upstream: upstream, Proxy: name, Protocol: choice.Protocol, State: "active"}})
			services = append(services, c.Service)
		}
		if err := compose.WriteAtomic(path, rewritten); err != nil {
			return nil, err
		}
		record := compose.Record{ProjectPath: filepath.Dir(path), ComposePath: path, Original: original, ExpectedSHA: sha(rewritten), Entries: entries}
		if err := m.store.Save(record); err != nil {
			_ = compose.WriteAtomic(path, original)
			return nil, err
		}
		if err := compose.Recreate(ctx, filepath.Dir(path), path, unique(services)); err != nil {
			_ = compose.WriteAtomic(path, original)
			_ = m.store.Remove(path)
			return nil, errors.New("docker compose could not recreate selected services")
		}
		current := applied{record: record, original: original, services: unique(services)}
		for _, e := range entries {
			if _, err := m.proxies.Create(config.Proxy{Name: e.Proxy, Active: true, Protocol: e.Protocol, Listen: e.Listen, Upstream: e.Upstream}); err != nil {
				for _, name := range current.proxies {
					_ = m.proxies.Delete(name)
				}
				_ = compose.WriteAtomic(path, original)
				_ = compose.Recreate(ctx, filepath.Dir(path), path, unique(services))
				_ = m.store.Remove(path)
				return nil, err
			}
			current.proxies = append(current.proxies, e.Proxy)
		}
		appliedGroups = append(appliedGroups, current)
	}
	deployments, err := m.deploymentsLocked()
	if err != nil {
		return nil, err
	}
	success = true
	return deployments, nil
}

// Restore restores exact saved files only when no operator edit has occurred.
func (m *ComposeManager) Restore(ids []string, all bool) ([]compose.Deployment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	records, err := m.store.Records()
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool)
	for _, id := range ids {
		wanted[id] = true
	}
	if !all && len(wanted) == 0 {
		return nil, errors.New("select a deployment to restore")
	}
	selected := make([]compose.Record, 0)
	for _, r := range records {
		take := all
		for _, e := range r.Entries {
			take = take || wanted[e.ID]
		}
		if take {
			selected = append(selected, r)
		}
	}
	if len(selected) == 0 {
		return nil, errors.New("managed deployment not found")
	}
	for _, r := range selected {
		b, err := osReadFile(r.ComposePath)
		if err != nil || sha(b) != r.ExpectedSHA {
			return nil, fmt.Errorf("Compose file drift detected for %s", r.ComposePath)
		}
	}
	ctx, cancel := compose.Context()
	defer cancel()
	for _, r := range selected {
		services := make([]string, 0, len(r.Entries))
		for _, e := range r.Entries {
			_ = m.proxies.Delete(e.Proxy)
			services = append(services, e.Service)
		}
		if err := compose.WriteAtomic(r.ComposePath, r.Original); err != nil {
			return nil, err
		}
		if err := compose.Recreate(ctx, r.ProjectPath, r.ComposePath, unique(services)); err != nil {
			return nil, errors.New("docker compose could not restore selected services")
		}
		if err := m.store.Remove(r.ComposePath); err != nil {
			return nil, err
		}
	}
	return m.deploymentsLocked()
}

var osReadFile = func(path string) ([]byte, error) { return os.ReadFile(path) }

func sha(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func revision(projects []compose.Project) string {
	var b strings.Builder
	for _, p := range projects {
		b.WriteString(p.ComposeFile)
		for _, c := range p.Candidates {
			b.WriteString(c.ID)
			b.WriteString(c.Listen)
			b.WriteString(c.Target)
		}
	}
	return sha([]byte(b.String()))[:16]
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func proxyName(c compose.Candidate) string {
	port := strings.TrimPrefix(strings.TrimPrefix(c.Listen, "0.0.0.0"), ":")
	base := unsafeName.ReplaceAllString(c.Project+"-"+c.Service+"-"+port, "-")
	if len(base) > 46 {
		base = base[:46]
	}
	return "ad-" + strings.Trim(base, "-") + "-" + c.ID[:8]
}
func unique(values []string) []string {
	set := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if !set[v] {
			set[v] = true
			out = append(out, v)
		}
	}
	return out
}
