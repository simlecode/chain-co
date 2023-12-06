package main

import (
	"context"
	"net"
	"os"
	"strings"

	"github.com/ipfs-force-community/metrics"

	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/lotus/api/v1api"
	vapi "github.com/filecoin-project/venus/venus-shared/api"
	local_api "github.com/ipfs-force-community/sophon-co/cli/api"

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
			Value: "venus-node-co",
		},
		&cli.StringFlag{
			Name:  "metrics-endpoint",
			Value: metrics.DefaultMetricsConfig().Exporter.Prometheus.EndPoint,
		},
	},
	Action: func(cctx *cli.Context) error {
		appCtx, appCancel := context.WithCancel(cctx.Context)
		defer appCancel()

		var full v1api.FullNode
		var localApi local_api.LocalAPI

		localJwt, token, err := jwtclient.NewLocalAuthClient()
		if err != nil {
			return err
		}
		err = os.WriteFile("./token", token, 0o666)
		if err != nil {
			return err
		}
		stop, err := service.Build(
			appCtx,

			dep.MetricsCtxOption(appCtx, cliName),

			dep.APIVersionOption(cctx.String("version")),
			service.ParseNodeInfoList(cctx.StringSlice("node"), cctx.String("version")),
			service.FullNode(&full),
			service.LocalAPI(&localApi),
		)
		if err != nil {
			return err
		}

		defer stop(context.Background()) // nolint:errcheck

		jCnf := &metrics.TraceConfig{}
		proxy, sampler, serverName := strings.TrimSpace(cctx.String("jaeger-proxy")),
			cctx.Float64("trace-sampler"),
			strings.TrimSpace(cctx.String("trace-node-name"))

		if jCnf.JaegerTracingEnabled = len(proxy) != 0; jCnf.JaegerTracingEnabled {
			jCnf.ProbabilitySampler, jCnf.JaegerEndpoint, jCnf.ServerName = sampler, proxy, serverName
		}

		mCfg := metrics.DefaultMetricsConfig()
		mCfg.Enabled = true
		mCfg.Exporter.Prometheus.Namespace = "chain_co"
		mCfg.Exporter.Graphite.Namespace = "chain_co"
		if cctx.IsSet("metrics-endpoint") {
			mCfg.Exporter.Prometheus.EndPoint = cctx.String("metrics-endpoint")
			addr, err := net.ResolveTCPAddr("tcp", cctx.String("metrics-endpoint"))
			if err != nil {
				return err
			}
			mCfg.Exporter.Graphite.Host = addr.IP.String()
			mCfg.Exporter.Graphite.Port = addr.Port
		}

		err = metrics.SetupMetrics(appCtx, mCfg)
		if err != nil {
			return err
		}

		authApi := vapi.ParseApiInfo(cctx.String("auth"))

		return serveRPC(
			appCtx,
			authApi,
			cctx.String("rate-limit-redis"),
			cctx.String("listen"),
			jCnf,
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
