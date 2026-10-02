package rpmdb

import (
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/anchore/go-rpmdb/pkg/bdb"
	"github.com/anchore/go-rpmdb/pkg/ndb"
)

// probing a db with the wrong format must not leak the file handle (windows can't delete a file that is still open)
func TestOpen_failedProbeClosesFile(t *testing.T) {
	fdDir := map[string]string{"linux": "/proc/self/fd", "darwin": "/dev/fd"}[runtime.GOOS]
	if fdDir == "" {
		t.Skip("no fd listing on this os")
	}
	openFDs := func() int {
		entries, err := os.ReadDir(fdDir)
		require.NoError(t, err)
		return len(entries)
	}

	before := openFDs()
	_, err := ndb.Open("testdata/centos7-plain/Packages") // a bdb file
	require.ErrorIs(t, err, ndb.ErrorInvalidNDB)
	_, err = bdb.Open("testdata/rockylinux-9/rpmdb.sqlite") // a sqlite file
	require.Error(t, err)
	require.Equal(t, before, openFDs())
}
