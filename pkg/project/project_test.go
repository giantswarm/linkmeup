package project

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	testTag       = "v0.7.10"
	testCommit    = "361cdefdb8c6e2e0e940def05b4b026ee524cff3"
	testTimestamp = "2026-10-11T09:12:00Z"
)

func stub(t *testing.T, stampedVersion, stampedSHA, stampedTimestamp string, recorded vcs) {
	t.Helper()
	origVersion, origSHA, origTimestamp, origReadVCS := version, gitSHA, buildTimestamp, readVCS
	t.Cleanup(func() {
		version, gitSHA, buildTimestamp, readVCS = origVersion, origSHA, origTimestamp, origReadVCS
	})
	version, gitSHA, buildTimestamp = stampedVersion, stampedSHA, stampedTimestamp
	readVCS = func() vcs { return recorded }
}

func TestVersion(t *testing.T) {
	for _, tc := range []struct {
		name     string
		stamped  string
		recorded vcs
		want     string
	}{
		{name: "nothing known", want: dev},
		{name: "ldflag without v gets it", stamped: "0.7.10", want: testTag},
		{name: "ldflag with v is kept", stamped: testTag, want: testTag},
		{name: "ldflag is trimmed", stamped: " 0.8.0-rc.1 ", want: "v0.8.0-rc.1"},
		{name: "ldflag wins over build info", stamped: "0.8.0", recorded: vcs{version: testTag}, want: "v0.8.0"},
		{name: "build info supplies the tag", recorded: vcs{version: testTag}, want: testTag},
		{name: "commit alone is no version", recorded: vcs{revision: testCommit}, want: dev},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub(t, tc.stamped, "", "", tc.recorded)
			require.Equal(t, tc.want, Version())
		})
	}
}

func TestCommitAndTimestamp(t *testing.T) {
	t.Run("stamped values win", func(t *testing.T) {
		stub(t, "", "abc1234def", "2026-10-12T00:00:00Z", vcs{revision: testCommit, time: testTimestamp})
		require.Equal(t, "abc1234def", GitSHA())
		require.Equal(t, "abc1234", ShortSHA())
		require.Equal(t, "2026-10-12T00:00:00Z", BuildTimestamp())
	})

	t.Run("build info is the fallback", func(t *testing.T) {
		stub(t, "", "", "", vcs{revision: testCommit, time: testTimestamp})
		require.Equal(t, testCommit, GitSHA())
		require.Equal(t, "361cdef", ShortSHA())
		require.Equal(t, testTimestamp, BuildTimestamp())
	})

	t.Run("unknown is empty", func(t *testing.T) {
		stub(t, "", "", "", vcs{})
		require.Empty(t, GitSHA())
		require.Empty(t, ShortSHA())
		require.Empty(t, BuildTimestamp())
	})
}

func TestVersionLine(t *testing.T) {
	t.Run("all details", func(t *testing.T) {
		stub(t, "0.7.10", testCommit, testTimestamp, vcs{})
		require.Equal(t, "v0.7.10 (commit 361cdef, built 2026-10-11T09:12:00Z)", VersionLine())
	})

	t.Run("no details", func(t *testing.T) {
		stub(t, "", "", "", vcs{})
		require.Equal(t, "dev", VersionLine())
	})
}
