package control

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/lentscode/ctf-proxy/internal/compose"
	"github.com/lentscode/ctf-proxy/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAPIScanAndConfigureApplyAndRestore(t *testing.T) {
	root, _, handler, calls := newComposeAPITestServer(t, false)
	composePath := writeComposeFixture(t, root, "compose.yaml")
	original, err := os.ReadFile(composePath)
	require.NoError(t, err)

	response := serveAPI(handler, http.MethodGet, "/api/v1/scan-and-configure/projects", "")
	require.Equal(t, http.StatusOK, response.Code)
	var discovery struct {
		Projects []compose.Project `json:"projects"`
		Revision string            `json:"revision"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &discovery))
	require.Len(t, discovery.Projects, 1)
	candidate := discovery.Projects[0].Candidates[0]
	require.True(t, candidate.Eligible)

	response = serveAPI(handler, http.MethodPost, "/api/v1/scan-and-configure/apply", `{"revision":"`+discovery.Revision+`","selections":[{"id":"`+candidate.ID+`","protocol":"tcp"}]}`)
	require.Equal(t, http.StatusOK, response.Code)
	rewritten, err := os.ReadFile(composePath)
	require.NoError(t, err)
	require.Contains(t, string(rewritten), "127.0.0.1:")
	require.NotEqual(t, original, rewritten)
	require.GreaterOrEqual(t, len(*calls), 3) // version, config, up

	response = serveAPI(handler, http.MethodGet, "/api/v1/scan-and-configure/deployments", "")
	require.Equal(t, http.StatusOK, response.Code)
	var deployments struct {
		Deployments []compose.Deployment `json:"deployments"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &deployments))
	require.Len(t, deployments.Deployments, 1)
	proxy := deployments.Deployments[0].Proxy
	response = serveAPI(handler, http.MethodGet, "/api/v1/proxies/"+proxy, "")
	require.Equal(t, http.StatusOK, response.Code)

	response = serveAPI(handler, http.MethodPost, "/api/v1/scan-and-configure/restore", `{"ids":["`+candidate.ID+`"]}`)
	require.Equal(t, http.StatusOK, response.Code)
	restored, err := os.ReadFile(composePath)
	require.NoError(t, err)
	require.Equal(t, original, restored)
	response = serveAPI(handler, http.MethodGet, "/api/v1/proxies/"+proxy, "")
	require.Equal(t, http.StatusNotFound, response.Code)
}

func TestAPIScanAndConfigureRejectsInvalidApplyRequests(t *testing.T) {
	tests := []struct {
		name string
		body func(revision, id string) string
	}{
		{name: "stale revision", body: func(_ string, id string) string {
			return `{"revision":"old","selections":[{"id":"` + id + `","protocol":"tcp"}]}`
		}},
		{name: "unknown mapping", body: func(revision, _ string) string {
			return `{"revision":"` + revision + `","selections":[{"id":"missing","protocol":"tcp"}]}`
		}},
		{name: "HTTP without scheme", body: func(revision, id string) string {
			return `{"revision":"` + revision + `","selections":[{"id":"` + id + `","protocol":"http"}]}`
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, _, handler, _ := newComposeAPITestServer(t, false)
			writeComposeFixture(t, root, "docker-compose.yml")
			response := serveAPI(handler, http.MethodGet, "/api/v1/scan-and-configure/projects", "")
			var discovery struct {
				Projects []compose.Project `json:"projects"`
				Revision string            `json:"revision"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &discovery))
			response = serveAPI(handler, http.MethodPost, "/api/v1/scan-and-configure/apply", test.body(discovery.Revision, discovery.Projects[0].Candidates[0].ID))
			require.Equal(t, http.StatusBadRequest, response.Code)
		})
	}
}

func TestAPIScanAndConfigureBlocksDriftedRestore(t *testing.T) {
	root, _, handler, _ := newComposeAPITestServer(t, false)
	composePath := writeComposeFixture(t, root, "docker-compose.yml")
	response := serveAPI(handler, http.MethodGet, "/api/v1/scan-and-configure/projects", "")
	var discovery struct {
		Projects []compose.Project `json:"projects"`
		Revision string            `json:"revision"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &discovery))
	candidate := discovery.Projects[0].Candidates[0]
	response = serveAPI(handler, http.MethodPost, "/api/v1/scan-and-configure/apply", `{"revision":"`+discovery.Revision+`","selections":[{"id":"`+candidate.ID+`","protocol":"tcp"}]}`)
	require.Equal(t, http.StatusOK, response.Code)
	require.NoError(t, os.WriteFile(composePath, []byte("services: {}\n# operator change\n"), 0o600))
	response = serveAPI(handler, http.MethodPost, "/api/v1/scan-and-configure/restore", `{"ids":["`+candidate.ID+`"]}`)
	require.Equal(t, http.StatusBadRequest, response.Code)
	response = serveAPI(handler, http.MethodGet, "/api/v1/scan-and-configure/deployments", "")
	require.Contains(t, response.Body.String(), "drifted")
}

func TestAPIScanAndConfigureRollsBackComposeFailure(t *testing.T) {
	root, _, handler, _ := newComposeAPITestServer(t, true)
	composePath := writeComposeFixture(t, root, "compose.yml")
	original, err := os.ReadFile(composePath)
	require.NoError(t, err)
	response := serveAPI(handler, http.MethodGet, "/api/v1/scan-and-configure/projects", "")
	var discovery struct {
		Projects []compose.Project `json:"projects"`
		Revision string            `json:"revision"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &discovery))
	candidate := discovery.Projects[0].Candidates[0]
	response = serveAPI(handler, http.MethodPost, "/api/v1/scan-and-configure/apply", `{"revision":"`+discovery.Revision+`","selections":[{"id":"`+candidate.ID+`","protocol":"tcp"}]}`)
	require.Equal(t, http.StatusBadRequest, response.Code)
	current, err := os.ReadFile(composePath)
	require.NoError(t, err)
	require.Equal(t, original, current)
	response = serveAPI(handler, http.MethodGet, "/api/v1/scan-and-configure/deployments", "")
	var deployments struct {
		Deployments []compose.Deployment `json:"deployments"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &deployments))
	require.Empty(t, deployments.Deployments)
}

func newComposeAPITestServer(t *testing.T, failUp bool) (string, string, http.Handler, *[]string) {
	t.Helper()
	temporary := t.TempDir()
	root := filepath.Join(temporary, "services")
	require.NoError(t, os.Mkdir(root, 0o755))
	path := filepath.Join(temporary, "ctf-proxy.yaml")
	store, err := config.OpenOrCreateStore(path)
	require.NoError(t, err)
	manager, err := NewManager(store, path)
	require.NoError(t, err)
	require.NoError(t, manager.Start(context.Background()))
	t.Cleanup(manager.Close)
	calls := []string{}
	previous := compose.RunCompose
	compose.RunCompose = func(_ context.Context, _ string, args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		if failUp && strings.Contains(strings.Join(args, " "), " up ") {
			return errors.New("fake compose failure")
		}
		return nil
	}
	t.Cleanup(func() { compose.RunCompose = previous })
	composeManager := NewComposeManager(root, path, manager)
	return root, path, NewHandlerWithScanAndConfigure(manager, []string{"test-token"}, nil, composeManager), &calls
}

func writeComposeFixture(t *testing.T, root, fileName string) string {
	t.Helper()
	project := filepath.Join(root, "demo")
	require.NoError(t, os.Mkdir(project, 0o755))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	path := filepath.Join(project, fileName)
	require.NoError(t, os.WriteFile(path, []byte("services:\n  web:\n    image: example\n    ports:\n      - \""+strconv.Itoa(port)+":80\"\n"), 0o600))
	return path
}
