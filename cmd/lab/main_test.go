package main

import "testing"

func TestTrafficRequestsIncludesEveryService(t *testing.T) {
	requests := trafficRequests()
	if len(requests) != len(services) {
		t.Fatalf("got %d traffic requests, want %d", len(requests), len(services))
	}

	for index, service := range services {
		if requests[index].service != service {
			t.Fatalf("request %d targets %q, want %q", index, requests[index].service, service)
		}
	}
}
