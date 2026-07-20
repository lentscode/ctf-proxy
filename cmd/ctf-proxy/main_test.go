package main

import "testing"

func TestDefaultControlAddressIsLoopback(t *testing.T) {
	if defaultControlAddr != "127.0.0.1:8081" {
		t.Fatalf("unexpected default control address %q", defaultControlAddr)
	}
}
