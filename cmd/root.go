package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/giantswarm/linkmeup/pkg/conf"
	"github.com/giantswarm/linkmeup/pkg/pacserver"
	"github.com/giantswarm/linkmeup/pkg/project"
	"github.com/giantswarm/linkmeup/pkg/proxy"
	"github.com/giantswarm/linkmeup/pkg/tshstatus"
	"github.com/giantswarm/linkmeup/pkg/tui"

	"github.com/charmbracelet/x/term"
	"github.com/lmittmann/tint"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const pacPort = 9999

var (
	// Used for flags.
	cfgFile  string
	logLevel string
	noTUI    bool
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
		Version: project.VersionLine(),
		RunE:    runRootCommand,
	}

	logger *slog.Logger
)

// Execute executes the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.SetVersionTemplate(`{{printf "` + project.Name + ` version %s\n" .Version}}`)
	rootCmd.AddCommand(newVersionCmd())

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default $HOME/.config/linkmeup.yaml)")
	rootCmd.Flags().BoolVar(&noTUI, "no-tui", false, "run without the terminal UI (automatic when stdin is not a terminal)")
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
	if headlessMode(noTUI, term.IsTerminal(os.Stdin.Fd())) {
		logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	} else {
		logger = slog.New(tint.NewTextHandler(os.Stdout, &tint.Options{
			Level:      level,
			TimeFormat: "Jan 02 15:04:05",
		}))
	}

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
	initConfig()
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

	headless := headlessMode(noTUI, term.IsTerminal(os.Stdin.Fd()))
	if !headless {
		// Silence the logger during TUI operation.
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	ctx := cmd.Context()
	if headless {
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
		defer stop()
	}

	proxies, err := startProxies()
	if err != nil {
		return err
	}

	defer stopProxies(proxies)

	server, err := startWebserver(proxies)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("Failed to stop PAC server", slog.String("error", err.Error()))
		}
	}()

	if headless {
		logger.Info("Running without terminal UI")
		<-ctx.Done()
		logger.Info("Stopping linkmeup")
		return nil
	}

	// Bubble Tea handles terminal signals; defers clean up on every return.
	if err := tui.Run(proxies, pacPort); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	return nil
}

func headlessMode(noTUI, stdinIsTerminal bool) bool {
	return noTUI || !stdinIsTerminal
}

func stopProxies(proxies []*proxy.Proxy) {
	for _, p := range proxies {
		err := p.Close()
		if err != nil {
			logger.Error("Failed to stop proxy", slog.String("name", p.Name), slog.String("error", err.Error()))
			continue
		}
	}
}

// Starts a Teleport port-forward process for each entry in privateInstallations.
func startProxies() ([]*proxy.Proxy, error) {
	started := make([]*proxy.Proxy, 0, len(config.Installations))
	for _, inst := range config.Installations {
		checkEndpoint := fmt.Sprintf("https://happaapi.%s/healthz", inst.Domain)
		p, err := proxy.New(logger, inst.Name, inst.Domain, checkEndpoint)
		if err != nil {
			// proxy.New starts a tunnel, so the ones already created would
			// keep running after this error reaches the caller.
			stopProxies(started)
			return nil, fmt.Errorf("failed to start proxy for %s: %w", inst.Name, err)
		}

		logger.Info("Proxy configured", slog.String("name", p.Name), slog.String("domain", p.Domain), slog.Int("port", p.Port))
		p.PingConstantly()

		started = append(started, p)
	}
	return started, nil
}

func startWebserver(proxies []*proxy.Proxy) (*pacserver.PacServer, error) {
	server, err := pacserver.New(logger, proxies, pacPort)
	if err != nil {
		return nil, fmt.Errorf("failed to create PAC server: %w", err)
	}

	if err := server.Serve(); err != nil {
		return nil, fmt.Errorf("failed to start PAC server: %w", err)
	}
	return server, nil
}
