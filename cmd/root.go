package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/giantswarm/linkmeup/pkg/pinger"
	"github.com/giantswarm/linkmeup/pkg/proxy"

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
	// Proxy ports start getting occupied from here.
	baseProxyPort = 1080

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
	logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
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

	err = startWebserver()
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

func startWebserver() error {
	// Generate PAC from privateInstallations and port numbers.
	pac := "function FindProxyForURL(url, host) {"
	for i, inst := range config.Installations {
		pac += fmt.Sprintf("\n  if (dnsDomainIs(host, '%s')) { return 'SOCKS5 localhost:%d'; }", inst.Domain, baseProxyPort+i)
	}
	pac += "\n  return 'DIRECT';\n}\n"

	// Create web server to serve PAC on port 9999
	logger.Info("Serving proxy auto-configuration (PAC) file", slog.String("url", "http://localhost:9999/proxy.pac"))
	http.HandleFunc("/proxy.pac", func(w http.ResponseWriter, r *http.Request) {
		logger.Debug("Serving request to PAC file", slog.String("url", r.URL.String()))
		w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
		_, _ = fmt.Fprint(w, pac)
	})

	go func() {
		server := &http.Server{
			Addr:         ":9999",
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			IdleTimeout:  60 * time.Second,
		}
		err := server.ListenAndServe()
		if err != nil {
			logger.Error("Auto-configuration web server error", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	// Set up channel to capture signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for termination signal
	logger.Info("Press Ctrl+C to quit.")
	<-sigChan
	logger.Info("Shutting down proxies and auto-configuration server")

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

	// Log the result
	if result.Success {
		logger.Debug("Ping successful", slog.String("name", prx.Name), slog.Int("response_code", result.ResponseCode), slog.Duration("duration", result.Duration))
	} else {
		if result.Error != nil {
			logger.Error("Ping failed", slog.String("name", prx.Name), slog.String("error", result.Error.Error()), slog.Duration("duration", result.Duration))
		} else {
			logger.Debug("Ping response", slog.String("name", prx.Name), slog.Int("response_code", result.ResponseCode), slog.Duration("duration", result.Duration))
		}
	}
}
