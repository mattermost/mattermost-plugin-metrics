package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/mattermost/mattermost/server/v8/platform/shared/filestore"
	"github.com/stretchr/testify/require"
)

func TestDeleteDump(t *testing.T) {
	fs, err := filestore.NewFileBackend(filestore.FileBackendSettings{
		DriverName: "local",
		Directory:  t.TempDir(),
	})
	require.NoError(t, err)

	testFile := "test.txt"
	_, err = fs.WriteFile(bytes.NewReader([]byte("random text")), testFile)
	require.NoError(t, err)

	plugin := &Plugin{
		fileBackend: fs,
	}

	t.Run("Successfully delete dump directory", func(t *testing.T) {
		job := &DumpJob{
			DumpLocation: filepath.Join(pluginDataDir, PluginName, "some-dump"),
		}

		success, err := job.DeleteDump(plugin)
		require.NoError(t, err)
		require.True(t, success)
	})

	t.Run("Dump location not under plugin-data directory", func(t *testing.T) {
		job := &DumpJob{
			DumpLocation: "/some-other-dir/some-dump",
		}

		success, err := job.DeleteDump(plugin)
		require.NoError(t, err)
		require.False(t, success)
	})

	t.Run("Empty dump location", func(t *testing.T) {
		job := &DumpJob{
			DumpLocation: "",
		}

		success, err := job.DeleteDump(plugin)
		require.NoError(t, err)
		require.False(t, success)

		// should not delete other files
		b, err := fs.ReadFile(testFile)
		require.NoError(t, err)
		require.Equal(t, "random text", string(b))
	})
}
