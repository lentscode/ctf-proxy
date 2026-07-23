package main

import (
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEcho(t *testing.T) {
	testCases := []struct {
		name    string
		payload string
	}{
		{name: "text payload", payload: "echo-check"},
		{name: "binary payload", payload: "\x00echo\xff"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server, client := net.Pipe()
			go echo(server)
			t.Cleanup(func() { _ = client.Close() })

			_, err := client.Write([]byte(testCase.payload))
			require.NoError(t, err)
			result := make([]byte, len(testCase.payload))
			_, err = io.ReadFull(client, result)
			require.NoError(t, err)
			require.Equal(t, testCase.payload, string(result))
		})
	}
}
