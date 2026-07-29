package compose

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiscoverAndRewritePublicTCPPort(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "service")
	require.NoError(t, os.Mkdir(project, 0o755))
	path := filepath.Join(project, ComposeFileName)
	source := []byte("services:\n  web:\n    image: example\n    ports:\n      - '8080:80'\n      - '127.0.0.1:9000:90'\n      - '5353:5353/udp'\n")
	require.NoError(t, os.WriteFile(path, source, 0o600))

	projects, err := Discover(root)
	require.NoError(t, err)
	require.Len(t, projects, 1)
	require.Len(t, projects[0].Candidates, 3)
	public := projects[0].Candidates[0]
	require.True(t, public.Eligible)
	require.Equal(t, ":8080", public.Listen)
	require.Equal(t, "80", public.Target)
	require.Contains(t, public.Upstream, "127.0.0.1:")
	require.False(t, projects[0].Candidates[1].Eligible)
	require.False(t, projects[0].Candidates[2].Eligible)

	rewritten, err := Rewrite(source, map[string]Candidate{public.ID: public})
	require.NoError(t, err)
	require.Contains(t, string(rewritten), "127.0.0.1:")
	require.NotContains(t, string(rewritten), "8080:80")
}

func TestDiscoverPortFormsAndSkipReasons(t *testing.T) {
	tests := []struct {
		name, service          string
		definition             string
		eligible               bool
		listen, target, reason string
	}{
		{name: "short TCP wildcard", service: "web", definition: `ports: ["18080:80"]`, eligible: true, listen: ":18080", target: "80"},
		{name: "long TCP wildcard", service: "web", definition: "ports:\n  - target: 81\n    published: \"18081\"\n    host_ip: 0.0.0.0\n    protocol: tcp", eligible: true, listen: "0.0.0.0:18081", target: "81"},
		{name: "UDP mapping", service: "udp", definition: `ports: ["18082:82/udp"]`, reason: "TCP"},
		{name: "port range", service: "range", definition: `ports: ["18083-18084:83-84"]`, reason: "numeric"},
		{name: "host network", service: "host-net", definition: "network_mode: host\nports: [\"18085:85\"]", reason: "host-network"},
		{name: "loopback only", service: "local", definition: `ports: ["127.0.0.1:18086:86"]`, reason: "loopback"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			project := filepath.Join(root, "service")
			require.NoError(t, os.Mkdir(project, 0o755))
			source := "services:\n  " + test.service + ":\n" + indentYAML(test.definition, "    ") + "\n"
			require.NoError(t, os.WriteFile(filepath.Join(project, ComposeFileName), []byte(source), 0o600))
			projects, err := Discover(root)
			require.NoError(t, err)
			candidate := projects[0].Candidates[0]
			require.Equal(t, test.eligible, candidate.Eligible)
			if test.eligible {
				require.Equal(t, test.listen, candidate.Listen)
				require.Equal(t, test.target, candidate.Target)
			} else {
				require.Contains(t, candidate.Reason, test.reason)
			}
		})
	}
}

func TestDiscoverAssignsDistinctPrivatePortsAcrossProjects(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"first", "second"} {
		project := filepath.Join(root, name)
		require.NoError(t, os.Mkdir(project, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(project, ComposeFileName), []byte("services:\n  web:\n    image: example\n    ports: [\"18080:80\"]\n"), 0o600))
	}
	projects, err := Discover(root)
	require.NoError(t, err)
	require.Len(t, projects, 2)
	first := projects[0].Candidates[0]
	second := projects[1].Candidates[0]
	require.True(t, first.Eligible)
	require.True(t, second.Eligible)
	require.NotEqual(t, first.Upstream, second.Upstream)
}

func indentYAML(value, prefix string) string {
	return prefix + strings.ReplaceAll(value, "\n", "\n"+prefix)
}

func TestStoreRoundTripsPrivateRecord(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "ctf-proxy.yaml")
	store := NewStore(configPath)
	record := Record{ComposePath: "/tmp/project/compose.yaml", Original: []byte("secret-compose"), ExpectedSHA: "expected", Entries: []Entry{{Deployment: Deployment{ID: "entry", State: "active"}}}}
	require.NoError(t, store.Save(record))
	records, err := store.Records()
	require.NoError(t, err)
	require.Equal(t, record, records[0])
	data, err := os.ReadFile(store.path(record.ComposePath))
	require.NoError(t, err)
	var persisted map[string]any
	require.NoError(t, json.Unmarshal(data, &persisted))
	info, err := os.Stat(store.path(record.ComposePath))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	require.NoError(t, store.Remove(record.ComposePath))
	records, err = store.Records()
	require.NoError(t, err)
	require.Empty(t, records)
}

func TestDiscoverSkipsNestedProject(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "outer", "inner")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(nested, ComposeFileName), []byte("services: {}\n"), 0o600))
	projects, err := Discover(root)
	require.NoError(t, err)
	require.Empty(t, projects)
}

func TestDiscoverSupportsAllDefaultNamesAndExtraSafeNames(t *testing.T) {
	root := t.TempDir()
	for index, name := range append(append([]string(nil), DefaultFileNames...), "custom-compose.yml") {
		dir := filepath.Join(root, string(rune('a'+index)))
		require.NoError(t, os.Mkdir(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("services: {}\n"), 0o600))
	}
	projects, err := DiscoverWithNames(root, append(DefaultFileNames, "custom-compose.yml", "../outside.yml"))
	require.NoError(t, err)
	require.Len(t, projects, 5)
}
