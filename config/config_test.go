package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Nodes = append(cfg.Nodes, []NodeConfig{
		{TokenURL: "token:/ip4/127.0.0.1/tcp/3453"},
		{TokenURL: "token:/ip4/127.0.0.1/tcp/3453"},
	}...)
	cfg.Auth.URL = "http://127.0.0.1:8989"
	cfg.RateLimit.Redis = "http://127.0.0.1:6379"
	cfg.Trace.JaegerEndpoint = "http://127.0.0.1:14268/api/traces"

	err := WriteConfig("./config_example.toml", cfg)
	assert.NoError(t, err)
}
