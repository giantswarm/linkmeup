package cmd

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/giantswarm/linkmeup/pkg/conf"
	"github.com/giantswarm/linkmeup/pkg/pacserver"
	"github.com/giantswarm/linkmeup/pkg/proxy"
	"github.com/giantswarm/linkmeup/pkg/tshstatus"
	"github.com/giantswarm/linkmeup/pkg/tui"

	"github.com/lmittmann/tint"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const pacPort = 9999

var (
	// Used for flags.
	cfgFile  string
	logLevel string
	config   conf.Config

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

	// Build login command to show to user in case of error
	teleportProxy := config.Teleport.Proxy
	if teleportProxy == "" {
		teleportProxy = "PROXY"
	}
	auth := config.Teleport.Auth
	if auth == "" {
		auth = "AUTH"
	}
	loginCmd := fmt.Sprintf("tsh login --proxy %s --auth %s", teleportProxy, auth)

	status, err := tshstatus.GetStatus(logger)
	if err != nil {
		if errors.Is(err, tshstatus.ErrNotLoggedIn) || errors.Is(err, tshstatus.ErrActiveProfileExpired) {
			fmt.Printf("Error: You are not logged in to Teleport. Please log in using '%s'.\n", loginCmd)
			os.Exit(1)
		}
		if errors.Is(err, tshstatus.ErrNoValidKeyPair) {
			fmt.Printf("Error: Your Teleport key pair is not valid. Please log out using 'tsh logout' and then log in using '%s'.\n", loginCmd)
			os.Exit(1)
		}

		return fmt.Errorf("failed to get tsh status: %w", err)
	}

	if status == nil || status.Active == nil || status.Active.ProfileURL == "" {
		fmt.Println("Error: No active Teleport profile found. Please log in using 'tsh login'.")
		os.Exit(1)
	}
	logger.Debug("Active Teleport profile found", slog.String("cluster", status.Active.Cluster), slog.Time("valid_until", status.Active.ValidUntil))

	// Silence the logger during TUI operation - redirect to discard
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	proxies, err := startProxies()
	if err != nil {
		return err
	}

	err = startWebserver(proxies)
	if err != nil {
		return err
	}

	// Set up signal handling for graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		stopProxies(proxies)
		os.Exit(0)
	}()

	// Run the TUI - this blocks until the user quits
	err = tui.Run(proxies, pacPort)
	if err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	// Clean up proxies when TUI exits
	stopProxies(proxies)

	return nil
}

func stopProxies(proxies []*proxy.Proxy) {
	for _, p := range proxies {
		err := p.Stop()
		if err != nil {
			// Can't log to TUI anymore, just continue
			continue
		}
	}
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

		p.PingConstantly()

		proxies = append(proxies, p)
	}
	return proxies, nil
}

func startWebserver(proxies []*proxy.Proxy) error {
	server, err := pacserver.New(logger, proxies, pacPort)
	if err != nil {
		return fmt.Errorf("failed to create PAC server: %w", err)
	}

	server.Serve()
	return nil
}
