package config

import (
	"fmt"
	"os"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// Config holds the application-wide configuration.
type Config struct {
	// HFToken is the Hugging Face API token.
	// It can be overridden by the HF_TOKEN environment variable if this is empty.
	HFToken string `yaml:"hf_token"` // Example YAML tag
}

// LoadConfig loads configuration from the specified file path.
// Currently, this is a placeholder and does not actually read from a file.
func LoadConfig(filePath string) (*Config, error) {
	logrus.Infof("Attempting to load configuration from: %s", filePath)

	// Read the configuration file
	yamlFile, err := os.ReadFile(filePath)
	if err != nil {
		// It's common to not find a config file, so treat this as a non-fatal warning
		// if the file simply doesn't exist. The application can then proceed with defaults.
		// However, if the file exists but is unreadable, that's a more serious issue.
		if os.IsNotExist(err) {
			logrus.Warnf("Configuration file %s not found. Proceeding with default/empty configuration.", filePath)
			return &Config{}, nil // Return empty config, not an error
		}
		return nil, fmt.Errorf("failed to read configuration file %s: %w", filePath, err)
	}

	// Initialize an empty config struct to unmarshal into
	var cfg Config

	// Unmarshal the YAML data into the Config struct
	err = yaml.Unmarshal(yamlFile, &cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal YAML from %s: %w", filePath, err)
	}

	logrus.Infof("Configuration loaded successfully from %s.", filePath)
	return &cfg, nil
}
