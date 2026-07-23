package main

import (
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEcho(t *testing.T) {
	server, client := net.Pipe()
	go echo(server)

	_, err := client.Write([]byte("echo-check"))
	require.NoError(t, err)
	result := make([]byte, len("echo-check"))
	_, err = io.ReadFull(client, result)
	require.NoError(t, err)
	require.Equal(t, "echo-check", string(result))
	require.NoError(t, client.Close())
}
