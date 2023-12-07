package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mitchellh/go-homedir"

	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/ipfs-force-community/metrics"
	local_api "github.com/ipfs-force-community/sophon-co/cli/api"
	"github.com/ipfs-force-community/sophon-co/config"

	"github.com/ipfs-force-community/sophon-auth/jwtclient"
	"github.com/ipfs-force-community/sophon-co/dep"
	"github.com/ipfs-force-community/sophon-co/service"
)

var runCmd = &cli.Command{
	Name:  "run",
	Usage: "start the sophon-co daemon",
	Flags: []cli.Flag{
		&cli.Int64Flag{
			Name:  "max-req-size",
			Usage: "max request size",
			Value: 10 << 20,
		},
		&cli.StringSliceFlag{
			Name:  "node",
			Usage: "node info, eg: token:node_url",
		},
		&cli.StringFlag{
			Name:  "auth",
			Usage: "sophon-auth api info , eg: token:http://xxx:xxx",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "rate-limit-redis",
			Usage: "config redis to request api limit",
		},
		&cli.StringFlag{
			Name:        "version",
			Usage:       "rpc api version",
			Value:       "v1",
			DefaultText: "v1",
		},
		&cli.StringFlag{
			Name: "jaeger-proxy",
		},
		&cli.Float64Flag{
			Name:  "trace-sampler",
			Value: 1.0,
		},
		&cli.StringFlag{
			Name:  "trace-node-name",
			Value: "sophon-co",
		},
	},
	Action: func(cctx *cli.Context) error {
		appCtx, appCancel := context.WithCancel(cctx.Context)
		defer appCancel()

		repoPath, err := homedir.Expand(cctx.String("repo"))
		if err != nil {
			return err
		}
		has, err := hasRepo(repoPath)
		if err != nil {
			return err
		}
		cfg := config.DefaultConfig()
		if has {
			cfg, err = config.ReadConfig(filepath.Join(repoPath, config.ConfigFile))
			if err != nil {
				return err
			}
		}

		if err := parseFlag(cctx, cfg); err != nil {
			return err
		}

		if !has {
			if err := os.MkdirAll(repoPath, 0o755); err != nil {
				return err
			}
			if err := config.WriteConfig(filepath.Join(repoPath, config.ConfigFile), cfg); err != nil {
				return err
			}
		}

		var full v1api.FullNode
		var localApi local_api.LocalAPI

		localJwt, token, err := jwtclient.NewLocalAuthClient()
		if err != nil {
			return err
		}
		err = os.WriteFile(filepath.Join(repoPath, config.TokenFile), token, 0o666)
		if err != nil {
			return err
		}
		stop, err := service.Build(
			appCtx,

			dep.MetricsCtxOption(appCtx, cliName),

			dep.APIVersionOption(cctx.String("version")),
			service.ParseNodeInfoList(config.NodeList(cfg.Nodes), cctx.String("version")),
			service.FullNode(&full),
			service.LocalAPI(&localApi),
		)
		if err != nil {
			return err
		}

		defer stop(context.Background()) // nolint:errcheck

		err = metrics.SetupMetrics(appCtx, cfg.Metrics)
		if err != nil {
			return err
		}

		return serveRPC(
			appCtx,
			cfg,
			localJwt,
			full,
			localApi,
			func(ctx context.Context) error {
				appCancel()
				stop(ctx) // nolint:errcheck
				return nil
			},
			cctx.Int64("max-req-size"),
		)
	},
}

func hasRepo(repoPath string) (bool, error) {
	_, err := os.Stat(repoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func parseFlag(cctx *cli.Context, cfg *config.Config) error {
	if cctx.IsSet("listen") {
		cfg.API.ListenAddress = cctx.String("listen")
	}
	if cctx.IsSet("auth") {
		urlToken := strings.SplitN(cctx.String("auth"), ":", 2)
		if len(urlToken) != 2 {
			return fmt.Errorf("invalid auth: %v", cctx.String("auth"))
		}
		cfg.Auth.Token = urlToken[0]
		cfg.Auth.URL = urlToken[1]
	}
	if cctx.IsSet("rate-limit-redis") {
		cfg.RateLimit.Redis = cctx.String("rate-limit-redis")
	}
	if cctx.IsSet("jaeger-proxy") {
		cfg.Trace.JaegerEndpoint = cctx.String("jaeger-proxy")
		cfg.Trace.JaegerTracingEnabled = true
	}
	if cctx.IsSet("trace-sampler") {
		cfg.Trace.ProbabilitySampler = cctx.Float64("trace-sampler")
	}
	if cctx.IsSet("trace-node-name") {
		cfg.Trace.ServerName = cctx.String("trace-node-name")
	}
	if cctx.IsSet("node") {
		var nodes []config.NodeConfig
		for _, node := range cctx.StringSlice("node") {
			nodes = append(nodes, config.NodeConfig{
				TokenURL: node,
			})
		}
		cfg.Nodes = nodes
	}

	return nil
}
