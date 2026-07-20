// Command ctf-proxy is the local control and data-plane process for a CTF vulnbox.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/lentscode/ctf-proxy/internal/config"
	"github.com/lentscode/ctf-proxy/internal/filter"
	"github.com/lentscode/ctf-proxy/internal/proxy"
)

const (
	defaultConfigPath     = "ctf-proxy.yaml"
	defaultMaxConnections = 128
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	configPath := os.Getenv("CTF_PROXY_CONFIG")
	if configPath == "" {
		configPath = defaultConfigPath
	}
	if err := run(ctx, configPath); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	runners, err := buildRunners(cfg, configPath)
	if err != nil {
		return err
	}
	if len(runners) == 0 {
		return fmt.Errorf("configuration contains no active proxies")
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, len(runners))
	var wg sync.WaitGroup
	for _, current := range runners {
		log.Printf("starting %s proxy %q on %s -> %s", current.protocol, current.name, current.listen, current.upstream)
		wg.Go(func() { errs <- current.runner.Start(ctx) })
	}
	go func() {
		wg.Wait()
		close(errs)
	}()

	for err := range errs {
		if err != nil {
			cancel()
			for range errs {
			}
			return err
		}
	}
	return nil
}

type namedRunner struct {
	name, protocol, listen, upstream string
	runner                           proxy.Runner
}

func buildRunners(cfg config.Config, configPath string) ([]namedRunner, error) {
	maxConnections := cfg.MaxConnections
	if maxConnections == 0 {
		maxConnections = defaultMaxConnections
	}

	baseDirectory := filepath.Dir(configPath)
	runners := make([]namedRunner, 0, len(cfg.Proxies))
	for _, definition := range cfg.Proxies {
		if !definition.Active {
			log.Printf("proxy %q is inactive; not starting", definition.Name)
			continue
		}
		filterPaths := make([]string, len(definition.FilterFiles))
		for index, path := range definition.FilterFiles {
			if !filepath.IsAbs(path) {
				path = filepath.Join(baseDirectory, path)
			}
			filterPaths[index] = path
		}
		filters, err := filter.LoadYAMLFiles(filterPaths)
		if err != nil {
			return nil, fmt.Errorf("load filters for proxy %q: %w", definition.Name, err)
		}
		chain, err := filter.NewChain(filters...)
		if err != nil {
			return nil, fmt.Errorf("build filter chain for proxy %q: %w", definition.Name, err)
		}

		current := namedRunner{
			name: definition.Name, protocol: definition.Protocol,
			listen: definition.Listen, upstream: definition.Upstream,
		}
		slots := make(chan struct{}, maxConnections)
		switch definition.Protocol {
		case "tcp":
			current.runner = proxy.NewTCPProxy(definition.Listen, definition.Upstream, slots, chain)
		case "http":
			current.runner = proxy.NewHTTPProxy(definition.Listen, definition.Upstream, slots, chain)
		default:
			return nil, fmt.Errorf("build proxy %q: unsupported protocol %q", definition.Name, definition.Protocol)
		}
		runners = append(runners, current)
	}
	return runners, nil
}
