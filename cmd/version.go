package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/giantswarm/linkmeup/pkg/project"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version, commit and build time of linkmeup",
		Long: `Prints the release version, the git commit and the build time of this
binary. A build between releases reports Go's pseudo-version; one without any
version information says dev. Reads no configuration.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s version %s\n", project.Name, project.VersionLine())
			return err
		},
	}
}
