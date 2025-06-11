package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/giantswarm/linkmeup/pkg/pacserver"
	"github.com/giantswarm/linkmeup/pkg/proxy"

	"github.com/lmittmann/tint"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	// Used for flags.
	cfgFile  string
	logLevel string
	config   Config

	rootCmd = &cobra.Command{
		Use:   "linkmeup",
		Short: "Provides teleport proxies to some installations",
		Long: `This commands creates proxies for access to some private installations.
it requires:

- Teleport and tsh to be installed
- Installation configuration
- Teleport set up to work with our teleport cluster

The command will serve a proxy auto configuration (PAC) file on

  http://localhost:9999/proxy.pac

You can use this to configure your browser or operating system to use the proxies.
`,
		RunE: runRootCommand,
	}

	logger *slog.Logger
)

// Execute executes the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default $HOME/.config/linkmeup.yaml)")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "set the log level (debug, info, warn, error)")
}

type Config struct {
	Installations []Installation `mapstructure:"installations"`
}

type Installation struct {
	Name   string `mapstructure:"name"`
	Domain string `mapstructure:"domain"`
}

func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory and current directory
		viper.SetConfigType("yaml")
		viper.AddConfigPath(filepath.Join(home, ".config"))
		viper.AddConfigPath(".")
		viper.SetConfigName("linkmeup")
	}

	viper.AutomaticEnv()

	// Add a logger to the root command
	level := slog.LevelInfo
	switch logLevel {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		fmt.Printf("Invalid log level: %s. Valid options are: debug, info, warn, error, fatal.\n", logLevel)
		os.Exit(1)
	}
	logger = slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level:      level,
		TimeFormat: "Jan 02 15:04:05",
	}))

	err := viper.ReadInConfig()
	if err != nil {
		logger.Error("Error reading config file", slog.String("error", err.Error()))
		os.Exit(1)
	}

	logger.Info("Using config file", slog.String("path", viper.ConfigFileUsed()))

	err = viper.Unmarshal(&config)
	if err != nil {
		logger.Error("Unable to decode into struct", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if len(config.Installations) == 0 {
		logger.Error("No installations found in config file")
		os.Exit(1)
	}
}

func runRootCommand(cmd *cobra.Command, args []string) error {
	logger.Debug("Starting linkmeup", slog.String("log_level", logLevel))

	proxies, err := startProxies()
	if err != nil {
		return err
	}

	err = startWebserver(proxies)
	if err != nil {
		return err
	}

	return nil
}

// Starts a Teleport port-forward process for each entry in privateInstallations.
func startProxies() ([]*proxy.Proxy, error) {
	proxies := make([]*proxy.Proxy, 0, len(config.Installations))
	for _, inst := range config.Installations {
		checkEndpoint := fmt.Sprintf("https://happaapi.%s/healthz", inst.Domain)
		p, err := proxy.New(logger, inst.Name, inst.Domain, checkEndpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to start proxy for %s: %w", inst.Name, err)
		}
		proxies = append(proxies, p)
	}
	return proxies, nil
}

func startWebserver(proxies []*proxy.Proxy) error {
	server, err := pacserver.New(logger, proxies, 9999)
	if err != nil {
		return fmt.Errorf("failed to create PAC server: %w", err)
	}

	server.Serve()
	return nil
}
