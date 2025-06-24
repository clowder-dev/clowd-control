package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aifoundry-org/clowd-control/pkg/config"
	"github.com/aifoundry-org/clowd-control/pkg/inventorymanager"
	"github.com/aifoundry-org/clowd-control/pkg/modelmanager"
	"github.com/aifoundry-org/clowd-control/pkg/opapi"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	// defaultK8sTargetNamespace is the default namespace for the KubernetesNodeProvider.
	// TODO: This should be configurable via the main configuration file.
	defaultK8sTargetNamespace = "clowd-control-dev-ns"
)

const (
	// defaultListenAddress is the default address for the API server.
	// TODO: This should be configurable via the main configuration file.
	defaultListenAddress = "127.0.0.1:8080"
	// defaultShutdownTimeout is the default time to wait for graceful server shutdown.
	defaultShutdownTimeout = 15 * time.Second
)

var (
	// configFile stores the path to the configuration file.
	configFile string
	// verbose enables or disables verbose logging.
	verbose bool
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "clowd-control",
	Short: "ClowdControl CLI application.",
	Long: `ClowdControl is a software used to control Clowder
GenAI distributed inference cluster, from managing models,
to making provisioning and load scheduling decisions.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Setup Logrus
		logrus.SetFormatter(&logrus.TextFormatter{
			FullTimestamp: true,
		})
		logrus.SetOutput(os.Stdout)

		if verbose {
			logrus.SetLevel(logrus.DebugLevel)
			logrus.Debugln("Verbose logging enabled.")
		} else {
			logrus.SetLevel(logrus.InfoLevel)
		}

		logrus.Infoln("Starting ClowdControl CLI...")
		logrus.Infof("Configuration file path: %s", configFile)

		// Attempt to load the configuration using the LoadConfig function.
		cfg, err := config.LoadConfig(configFile)
		if err != nil {
			logrus.Fatalf("Error loading configuration: %v", err)
		}

		// Log the loaded configuration.
		logrus.Infof("Configuration loaded: %+v", cfg)

		// Initialize ModelManager
		// Pass the HFToken from the loaded configuration.
		mm := modelmanager.NewModelManager(cfg.HFToken)
		logrus.Info("ModelManager initialized.")
		if cfg.HFToken != "" {
			logrus.Debug("ModelManager initialized with HFToken from configuration.")
		} else {
			logrus.Debug("ModelManager initialized without HFToken from configuration, will fallback to HF_TOKEN env var if needed.")
		}

		// Initialize InventoryManager
		im := inventorymanager.NewInventoryManager()
		logrus.Info("InventoryManager initialized.")

		// Initialize and register KubernetesNodeProvider
		// TODO: Make targetNamespace configurable (e.g., via cfg.KubernetesTargetNamespace)
		k8sProvider, err := inventorymanager.NewKubernetesNodeProvider(nil, defaultK8sTargetNamespace)
		if err != nil {
			logrus.Fatalf("Failed to create KubernetesNodeProvider: %v", err)
		}
		if err := im.RegisterNodeProvider("kubernetes", k8sProvider); err != nil {
			logrus.Fatalf("Failed to register KubernetesNodeProvider: %v", err)
		}
		logrus.Infof("KubernetesNodeProvider registered for namespace '%s'.", defaultK8sTargetNamespace)

		// Setup Operational API Server
		opapiCfg := opapi.Config{
			ListenAddress:    defaultListenAddress, // Using default, make this configurable later
			Logger:           logrus.StandardLogger(),
			ModelManager:     mm,
			InventoryManager: im, // Pass the initialized InventoryManager
		}
		// It's important to use the logger from the opapiCfg for consistency if it modifies it (e.g. adds fields)
		// However, NewServer currently uses the passed logger to create its own entry.
		// For now, logging directly via logrus global or a main-specific logger is fine.
		logrus.Infof("Attempting to start API Server on: %s. This should be made configurable.", opapiCfg.ListenAddress)

		apiServer, err := opapi.NewServer(opapiCfg)
		if err != nil {
			logrus.Fatalf("Failed to create API server: %v", err)
		}

		// Channel to listen for server errors from the Start method
		errChan := make(chan error, 1)
		// Channel to listen for OS signals for graceful shutdown
		quitChan := make(chan os.Signal, 1)
		signal.Notify(quitChan, syscall.SIGINT, syscall.SIGTERM)

		// Start the server in a goroutine
		go func() {
			logrus.Infof("Operational API server starting on %s", opapiCfg.ListenAddress)
			if err := apiServer.Start(); err != nil {
				// This error is typically http.ErrServerClosed on graceful shutdown,
				// or another error if ListenAndServe fails unexpectedly.
				errChan <- err
			}
		}()

		logrus.Info("ClowdControl is running. Press Ctrl+C to exit.")

		// Wait for either a server error or an OS signal
		select {
		case err := <-errChan:
			// Only fatal if it's an unexpected error. ErrServerClosed is normal on shutdown.
			if err != nil && err.Error() != "http: Server closed" { // http.ErrServerClosed.Error()
				logrus.Fatalf("API server failed: %v", err)
			}
		case sig := <-quitChan:
			logrus.Infof("Received signal: %s. Initiating shutdown...", sig)
		}

		// Perform graceful shutdown
		logrus.Info("Attempting to gracefully shut down the API server...")
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), defaultShutdownTimeout)
		defer cancelShutdown()

		if err := apiServer.Stop(shutdownCtx); err != nil {
			logrus.Errorf("API server graceful shutdown error: %v", err)
		} else {
			logrus.Info("API server stopped gracefully.")
		}
		logrus.Info("ClowdControl has shut down.")
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		logrus.Errorln(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "config.yaml", "Path to the configuration file (e.g., config.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging output")
}

func main() {
	Execute()
}
