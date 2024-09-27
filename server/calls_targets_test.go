package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResolveURL(t *testing.T) {
	ips, port, err := resolveURL("https://localhost:8045", time.Second)
	require.NoError(t, err)
	require.NotEmpty(t, ips)
	require.Equal(t, "127.0.0.1", ips[0].String())
	require.Equal(t, "8045", port)

	ips, port, err = resolveURL("http://127.0.0.1:8055", time.Second)
	require.NoError(t, err)
	require.NotEmpty(t, ips)
	require.Equal(t, "127.0.0.1", ips[0].String())
	require.Equal(t, "8055", port)
}
