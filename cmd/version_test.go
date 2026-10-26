package cmd

import (
	"bytes"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/linkmeup/pkg/project"
)

func TestVersionNeedsNoConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "subcommand", args: []string{"version"}},
		{name: "flag", args: []string{"--version"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			emptyDir := t.TempDir()
			t.Setenv("HOME", emptyDir)
			t.Chdir(emptyDir)
			viper.Reset()
			t.Cleanup(func() {
				if versionFlag := rootCmd.Flags().Lookup("version"); versionFlag != nil {
					require.NoError(t, versionFlag.Value.Set("false"))
				}
				rootCmd.SetArgs(nil)
				rootCmd.SetOut(nil)
			})

			var output bytes.Buffer
			rootCmd.SetOut(&output)
			rootCmd.SetArgs(tc.args)

			require.NoError(t, rootCmd.ExecuteContext(t.Context()))
			require.Equal(t, "linkmeup version "+project.VersionLine()+"\n", output.String())
			require.Empty(t, viper.ConfigFileUsed())
			require.Empty(t, config.Installations)
		})
	}
}
