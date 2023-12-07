package config

import (
	"os"

	"github.com/ipfs-force-community/metrics"
	"github.com/pelletier/go-toml"
)

const (
	ConfigFile = "config.toml"
	TokenFile  = "token"
)

type APIConfig struct {
	ListenAddress string
}

type AuthConfig struct {
	URL, Token string
}

type NodeConfig struct {
	TokenURL string
}

type RateLimitConfig struct {
	Redis string
}

type Config struct {
	API       APIConfig
	Auth      AuthConfig
	Nodes     []NodeConfig
	RateLimit RateLimitConfig
	Metrics   *metrics.MetricsConfig
	Trace     *metrics.TraceConfig
}

func DefaultConfig() *Config {
	cfg := &Config{
		API: APIConfig{
			ListenAddress: "0.0.0.0:1234",
		},
		Auth:    AuthConfig{},
		Metrics: metrics.DefaultMetricsConfig(),
		Trace:   metrics.DefaultTraceConfig(),
	}
	cfg.Metrics.Exporter.Prometheus.Namespace = "sophon-co"
	cfg.Metrics.Exporter.Graphite.Namespace = "sophon-co"
	cfg.Trace.JaegerEndpoint = "" // http://127.0.0.1:14268/api/traces
	cfg.Trace.ServerName = "sophon-co"

	return cfg
}

func NodeList(nodes []NodeConfig) []string {
	var list []string
	for _, node := range nodes {
		list = append(list, node.TokenURL)
	}
	return list
}

func ReadConfig(filePath string) (*Config, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	err = toml.Unmarshal(data, cfg)

	return cfg, err
}

func WriteConfig(filePath string, cfg *Config) error {
	data, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, data, 0o644)
}
