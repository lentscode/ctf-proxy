package main

import "testing"

func TestDefaultControlAddressIsLoopback(t *testing.T) {
	testCases := []struct {
		name string
		got  string
		want string
	}{
		{name: "control API", got: defaultControlAddr, want: "127.0.0.1:8081"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.got != testCase.want {
				t.Fatalf("unexpected default control address %q", testCase.got)
			}
		})
	}
}
