//go:build docker

package lab

import (
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func TestRealWorldLab(t *testing.T) {
	for _, command := range [][]string{{"docker", "compose", "version"}, {"python3", "--version"}, {pnpm(), "--version"}} {
		if err := exec.Command(command[0], command[1:]...).Run(); err != nil {
			t.Skipf("real-world lab requires %s", strings.Join(command, " "))
		}
	}
	lab := newLab(t)
	defer lab.cleanup()
	lab.setup()

	t.Run("direct_vulnerabilities", func(t *testing.T) {
		if result := lab.client("tcp-echo", "--message", "hello"); !result.OK || !result.Echoed {
			t.Fatalf("TCP echo normal request failed: %s", stableMapJSON(result))
		}
		if result := lab.client("tcp-echo", "--admin"); !result.OK || !result.FlagFormatValid {
			t.Fatalf("TCP echo admin vulnerability was not reachable: %s", stableMapJSON(result))
		}
		if result := lab.client("tcp-archive"); !result.OK {
			t.Fatalf("TCP archive normal request failed: %s", stableMapJSON(result))
		}
		if result := lab.client("tcp-archive", "--exploit"); !result.FlagFormatValid {
			t.Fatalf("TCP archive traversal was not reachable: %s", stableMapJSON(result))
		}
		if result := lab.client("http-login", "--username", "alice", "--password", "wonderland"); !result.OK {
			t.Fatalf("normal HTTP login failed: %s", stableMapJSON(result))
		}
		if result := lab.client("http-login", "--admin-exploit"); !result.FlagFormatValid {
			t.Fatalf("HTTP login vulnerability was not reachable: %s", stableMapJSON(result))
		}
		if result := lab.client("http-template"); !result.OK {
			t.Fatalf("normal template render failed: %s", stableMapJSON(result))
		}
		if result := lab.client("http-template", "--exploit"); !result.FlagFormatValid {
			t.Fatalf("HTTP template vulnerability was not reachable: %s", stableMapJSON(result))
		}
	})

	t.Run("dashboard_takeover", func(t *testing.T) {
		if err := lab.runPlaywright("takeover.spec.ts"); err != nil {
			t.Fatal(err)
		}
		lab.proxiesFromAPI()
		if result := lab.client("tcp-echo", "--admin"); !result.FlagFormatValid {
			t.Fatalf("TCP echo did not work after takeover: %s", stableMapJSON(result))
		}
		if result := lab.client("tcp-archive", "--exploit"); !result.FlagFormatValid {
			t.Fatalf("TCP archive did not work after takeover: %s", stableMapJSON(result))
		}
		if result := lab.client("http-login", "--admin-exploit"); !result.FlagFormatValid {
			t.Fatalf("HTTP login did not work after takeover: %s", stableMapJSON(result))
		}
		if result := lab.client("http-template", "--exploit"); !result.FlagFormatValid {
			t.Fatalf("HTTP template did not work after takeover: %s", stableMapJSON(result))
		}
	})

	t.Run("dashboard_filters_and_real_traffic", func(t *testing.T) {
		if err := lab.runPlaywright("filters.spec.ts"); err != nil {
			t.Fatal(err)
		}
		if result := lab.client("tcp-echo", "--admin"); !result.FlagFormatValid {
			t.Fatalf("unfiltered TCP echo unexpectedly failed: %s", stableMapJSON(result))
		}
		if result := lab.client("tcp-archive", "--exploit"); result.FlagFound {
			t.Fatalf("TCP archive traversal was not blocked: %s", stableMapJSON(result))
		}
		if result := lab.client("http-login", "--admin-exploit"); result.Status != http.StatusForbidden || result.FlagFound {
			t.Fatalf("HTTP login exploit was not blocked: %s", stableMapJSON(result))
		}
		if result := lab.client("http-template", "--exploit"); result.Status != http.StatusForbidden || result.FlagFound {
			t.Fatalf("HTTP template exploit was not blocked: %s", stableMapJSON(result))
		}
		request, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(lab.ports["http-template"])+"/healthz", nil)
		request.Header.Set("X-Lab-Probe", "blocked")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("send header probe: %v", err)
		}
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("header probe status = %d", response.StatusCode)
		}
		_ = response.Body.Close()
		for _, event := range lab.events() {
			encoded := stableMapJSON(event)
			if strings.Contains(encoded, "username=admin") || strings.Contains(encoded, "X-Lab-Probe") || strings.Contains(encoded, "{{flag}}") {
				t.Fatalf("event exposed traffic data: %s", encoded)
			}
		}
		rejected := 0
		for _, event := range lab.events() {
			if event["kind"] == "filter_rejected" {
				rejected++
			}
		}
		if rejected < 4 {
			t.Fatalf("filter rejection events = %d, want at least 4", rejected)
		}
	})

	t.Run("dashboard_events", func(t *testing.T) {
		if err := lab.runPlaywright("events.spec.ts"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("dashboard_restore", func(t *testing.T) {
		if err := lab.runPlaywright("takeover.spec.ts"); err != nil {
			t.Fatal(err)
		}
		var result struct {
			Proxies []any `json:"proxies"`
		}
		lab.api(http.MethodGet, "/api/v1/proxies", nil, &result)
		if len(result.Proxies) != 0 {
			t.Fatalf("proxies remained after restore: %d", len(result.Proxies))
		}
		if result := lab.client("tcp-echo", "--admin"); !result.FlagFormatValid {
			t.Fatalf("TCP echo did not return after restore: %s", stableMapJSON(result))
		}
		if result := lab.client("tcp-archive", "--exploit"); !result.FlagFormatValid {
			t.Fatalf("TCP archive did not return after restore: %s", stableMapJSON(result))
		}
		if result := lab.client("http-login", "--admin-exploit"); !result.FlagFormatValid {
			t.Fatalf("HTTP login did not return after restore: %s", stableMapJSON(result))
		}
		if result := lab.client("http-template", "--exploit"); !result.FlagFormatValid {
			t.Fatalf("HTTP template did not return after restore: %s", stableMapJSON(result))
		}
	})
}
