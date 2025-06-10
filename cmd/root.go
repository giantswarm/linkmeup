package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/giantswarm/linkmeup/pkg/pacserver"
	"github.com/giantswarm/linkmeup/pkg/pinger"
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

const (
	pingTimeout  = 20 * time.Second // Timeout for pinging proxies
	pingInterval = 30 * time.Second // Interval between pings
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

	go func() {
		time.Sleep(10 * time.Second) // Give proxies some time to start
		ctx := context.Background()
		err = startPinger(ctx, proxies)
		if err != nil {
			logger.Error("Error in pinger", slog.String("error", err.Error()))
		}
	}()

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
		p, err := proxy.New(logger, inst.Name, inst.Domain)
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

func startPinger(ctx context.Context, proxies []*proxy.Proxy) error {
	logger.Debug("Starting pinger")

	// Create a wait group to keep track of goroutines
	var wg sync.WaitGroup

	// Start a goroutine for each proxy
	for _, prx := range proxies {
		wg.Add(1)

		go func(prx *proxy.Proxy) {
			defer wg.Done()

			logger.Debug("Creating proxy pinger", slog.String("proxy", prx.Name), slog.String("proxy", prx.Domain), slog.Int("port", prx.Port))
			pingerConfig := pinger.Config{
				ProxyPort: prx.Port,
				Timeout:   pingTimeout,
			}
			proxyPinger, err := pinger.New(pingerConfig)
			if err != nil {
				logger.Error("Failed to create pinger", slog.String("proxy", prx.Name), slog.String("error", err.Error()))
				return
			}

			ticker := time.NewTicker(pingInterval)
			defer ticker.Stop()

			// Also ping once immediately
			pingProxy(ctx, proxyPinger, prx)

			// Then ping on each tick
			for {
				select {
				case <-ticker.C:
					pingProxy(ctx, proxyPinger, prx)
				case <-ctx.Done():
					return
				}
			}
		}(prx)
	}

	return nil
}

// pingProxy pings a proxy and logs the result
func pingProxy(ctx context.Context, p *pinger.Pinger, prx *proxy.Proxy) {
	// Create a context with timeout for this specific ping
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()

	// Ping the proxy
	url := fmt.Sprintf("https://happaapi.%s/healthz", prx.Domain)
	result := p.Ping(pingCtx, url)

	durationMs := result.Duration.Milliseconds()

	// Log the result
	if result.Success {
		logger.Debug("Ping successful", slog.String("name", prx.Name), slog.Int("response_code", result.ResponseCode), slog.Int64("duration", durationMs))
	} else {
		if result.Error != nil {
			logger.Error("Ping failed", slog.String("name", prx.Name), slog.String("error", result.Error.Error()), slog.Int64("duration", durationMs))
		} else {
			logger.Debug("Ping response", slog.String("name", prx.Name), slog.Int("response_code", result.ResponseCode), slog.Int64("duration", durationMs))
		}
	}
}
