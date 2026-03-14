package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	GRPCAddress    string
	OpenFGAAPIURL  string
	OpenFGAStoreID string
	OpenFGAModelID string
}

func FromEnv() (Config, error) {
	cfg := Config{}
	grpcAddress := strings.TrimSpace(os.Getenv("GRPC_ADDRESS"))
	if grpcAddress == "" {
		grpcAddress = ":50051"
	}
	cfg.GRPCAddress = grpcAddress

	cfg.OpenFGAAPIURL = strings.TrimSpace(os.Getenv("OPENFGA_API_URL"))
	if cfg.OpenFGAAPIURL == "" {
		return Config{}, fmt.Errorf("OPENFGA_API_URL must be set")
	}

	cfg.OpenFGAStoreID = strings.TrimSpace(os.Getenv("OPENFGA_STORE_ID"))
	if cfg.OpenFGAStoreID == "" {
		return Config{}, fmt.Errorf("OPENFGA_STORE_ID must be set")
	}

	cfg.OpenFGAModelID = strings.TrimSpace(os.Getenv("OPENFGA_MODEL_ID"))
	if cfg.OpenFGAModelID == "" {
		return Config{}, fmt.Errorf("OPENFGA_MODEL_ID must be set")
	}

	return cfg, nil
}
