package cmd

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	// Used for flags.
	cfgFile string
	config  Config

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
)

const (
	// Proxy ports start getting occupied from here.
	baseProxyPort = 1080
)

// Execute executes the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default $HOME/.config/linkmeup.yaml)")
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

	err := viper.ReadInConfig()
	if err != nil {
		fmt.Printf("Error when reading config file: %s\n", err)
		os.Exit(1)
	}

	fmt.Println("Using config file:", viper.ConfigFileUsed())

	err = viper.Unmarshal(&config)
	if err != nil {
		fmt.Printf("unable to decode into struct, %s", err)
		os.Exit(1)
	}

	if len(config.Installations) == 0 {
		fmt.Println("No installations found in config file.")
		os.Exit(1)
	}
}

func runRootCommand(cmd *cobra.Command, args []string) error {
	err := startProxies()
	if err != nil {
		return err
	}

	err = startWebserver()
	if err != nil {
		return err
	}

	return nil
}

// Starts a Teleport port-forward process for each entry in privateInstallations.
func startProxies() error {
	for i, inst := range config.Installations {
		port := baseProxyPort + i
		fmt.Printf("Starting proxy for %s (%s) on port %d\n", inst.Name, inst.Domain, port)
		host := fmt.Sprintf("root@role=control-plane,mc=%s", inst.Name)
		err := exec.Command("tsh", "ssh", "--no-remote-exec", "--dynamic-forward", fmt.Sprintf("%d", port), host).Start() //nolint:gosec
		if err != nil {
			return fmt.Errorf("failed to start proxy for %s: %v", inst.Name, err)
		}
	}
	return nil
}

func startWebserver() error {
	// Generate PAC from privateInstallations and port numbers.
	pac := "function FindProxyForURL(url, host) {"
	for i, inst := range config.Installations {
		pac += fmt.Sprintf("\n  if (dnsDomainIs(host, '%s')) { return 'SOCKS5 localhost:%d'; }", inst.Domain, baseProxyPort+i)
	}
	pac += "\n  return 'DIRECT';\n}\n"

	// Create web server to serve PAC on port 9999
	http.HandleFunc("/proxy.pac", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
		_, _ = fmt.Fprint(w, pac)
	})

	fmt.Printf("\nYour proxy auto-configuration URL:\n\n   http://localhost:9999/proxy.pac\n\n")
	fmt.Println("Please apply this URL in your system or browser settings.")

	go func() {
		server := &http.Server{
			Addr:         ":9999",
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			IdleTimeout:  60 * time.Second,
		}
		err := server.ListenAndServe()
		if err != nil {
			fmt.Printf("Auto-configuration web server error: %s\n", err)
			os.Exit(1)
		}
	}()

	// Set up channel to capture signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for termination signal
	fmt.Println("Press Ctrl+C to quit.")
	<-sigChan
	fmt.Println("\nShutting down proxies and auto-configuration server.")

	return nil
}
