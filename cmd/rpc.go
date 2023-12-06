package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"

	"github.com/dtynn/dix"
	"github.com/etherlabsio/healthcheck/v2"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v0api"
	vapi "github.com/filecoin-project/venus/venus-shared/api"
	"github.com/filecoin-project/venus/venus-shared/api/permission"
	"github.com/ipfs-force-community/metrics"
	"github.com/ipfs-force-community/metrics/ratelimit"
	"github.com/ipfs-force-community/sophon-auth/core"
	"github.com/ipfs-force-community/sophon-auth/jwtclient"
	local_api "github.com/ipfs-force-community/sophon-co/cli/api"
	logging "github.com/ipfs/go-log/v2"
	"go.opencensus.io/plugin/ochttp"
)

func serveRPC(ctx context.Context, authApi vapi.APIInfo, rateLimitRedis, listen string, mCnf *metrics.TraceConfig, jwt jwtclient.IJwtAuthClient, full api.FullNode, localApi local_api.LocalAPI, stop dix.StopFunc, maxRequestSize int64) error {
	serverOptions := []jsonrpc.ServerOption{}
	if maxRequestSize > 0 {
		serverOptions = append(serverOptions, jsonrpc.WithMaxRequestSize(maxRequestSize))
	}

	var remoteJwtCli *jwtclient.AuthClient
	if len(authApi.Addr) > 0 {
		if len(authApi.Token) == 0 {
			return fmt.Errorf("auth token is need when auth api is set")
		}
		remoteJwtCli, _ = jwtclient.NewAuthClient(authApi.Addr, string(authApi.Token))
	}

	pma := new(api.FullNodeStruct)
	permission.PermissionProxy(full, pma)
	if len(rateLimitRedis) > 0 && remoteJwtCli != nil {
		log.Infof("use rate limit %s", rateLimitRedis)
		limiter, err := ratelimit.NewRateLimitHandler(
			rateLimitRedis,
			nil, &core.ValueFromCtx{},
			jwtclient.WarpLimitFinder(remoteJwtCli),
			logging.Logger("rate-limit"))
		_ = logging.SetLogLevel("rate-limit", "debug")
		if err != nil {
			return err
		}

		var rateLimitAPI api.FullNodeStruct
		limiter.WrapFunctions(full, &rateLimitAPI.Internal)
		limiter.WrapFunctions(full, &rateLimitAPI.NetStruct.Internal)
		limiter.WrapFunctions(full, &rateLimitAPI.VenusAPIStruct.Internal)
		limiter.WrapFunctions(full, &rateLimitAPI.CommonStruct.Internal)
		pma = &rateLimitAPI
	}

	mux := http.NewServeMux()

	serveRpc := func(path string, hnd interface{}, rpcSer *jsonrpc.RPCServer, ethRPCAlias bool) {
		rpcSer.Register("Filecoin", hnd)

		if ethRPCAlias {
			createEthRPCAliases(rpcSer)
		}

		var handler http.Handler
		if remoteJwtCli != nil {
			handler = (http.Handler)(jwtclient.NewAuthMux(jwt, jwtclient.WarpIJwtAuthClient(remoteJwtCli), rpcSer))
		} else {
			handler = (http.Handler)(jwtclient.NewAuthMux(jwt, nil, rpcSer))
		}
		mux.Handle(path, handler)
	}

	serveRpc("/rpc/v0", &v0api.WrapperV1Full{FullNode: pma}, jsonrpc.NewServer(serverOptions...), false)
	serveRpc("/rpc/v1", pma, jsonrpc.NewServer(serverOptions...), true)
	serveRpc("/rpc/admin/v0", localApi, jsonrpc.NewServer(serverOptions...), false)
	mux.Handle("/healthcheck", healthcheck.Handler())

	allHandler := (http.Handler)(mux)

	if reporter, err := metrics.SetupJaegerTracing(mCnf.ServerName, mCnf); err != nil {
		log.Fatalf("register %s JaegerRepoter to %s failed:%s", mCnf.ServerName, mCnf.JaegerEndpoint, err)
	} else if reporter != nil {
		log.Infof("register jaeger-tracing exporter to %s, with node-name:%s", mCnf.JaegerEndpoint, mCnf.ServerName)
		defer metrics.ShutdownJaeger(ctx, reporter) //nolint:errcheck
		allHandler = &ochttp.Handler{Handler: allHandler}
	}

	server := http.Server{
		Addr:    listen,
		Handler: allHandler,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	sigCh := make(chan os.Signal, 2)

	go func() {
		select {
		case <-ctx.Done():

		case sig := <-sigCh:
			log.Infof("signal %s captured", sig)
		}

		if err := server.Shutdown(context.Background()); err != nil {
			log.Warnf("shutdown http server: %s", err)
		}

		if err := stop(context.Background()); err != nil {
			log.Warnf("call app stop func: %s", err)
		}

		log.Sync() // nolint:errcheck
	}()

	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	log.Infow("start http server", "addr", listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}

	log.Info("gracefull down")
	return nil
}

func createEthRPCAliases(as *jsonrpc.RPCServer) {
	// TODO: maybe use reflect to automatically register all the eth aliases
	as.AliasMethod("eth_accounts", "Filecoin.EthAccounts")
	as.AliasMethod("eth_blockNumber", "Filecoin.EthBlockNumber")
	as.AliasMethod("eth_getBlockTransactionCountByNumber", "Filecoin.EthGetBlockTransactionCountByNumber")
	as.AliasMethod("eth_getBlockTransactionCountByHash", "Filecoin.EthGetBlockTransactionCountByHash")

	as.AliasMethod("eth_getBlockByHash", "Filecoin.EthGetBlockByHash")
	as.AliasMethod("eth_getBlockByNumber", "Filecoin.EthGetBlockByNumber")
	as.AliasMethod("eth_getTransactionByHash", "Filecoin.EthGetTransactionByHash")
	as.AliasMethod("eth_getTransactionCount", "Filecoin.EthGetTransactionCount")
	as.AliasMethod("eth_getTransactionReceipt", "Filecoin.EthGetTransactionReceipt")
	as.AliasMethod("eth_getTransactionByBlockHashAndIndex", "Filecoin.EthGetTransactionByBlockHashAndIndex")
	as.AliasMethod("eth_getTransactionByBlockNumberAndIndex", "Filecoin.EthGetTransactionByBlockNumberAndIndex")

	as.AliasMethod("eth_getCode", "Filecoin.EthGetCode")
	as.AliasMethod("eth_getStorageAt", "Filecoin.EthGetStorageAt")
	as.AliasMethod("eth_getBalance", "Filecoin.EthGetBalance")
	as.AliasMethod("eth_chainId", "Filecoin.EthChainId")
	as.AliasMethod("eth_syncing", "Filecoin.EthSyncing")
	as.AliasMethod("eth_feeHistory", "Filecoin.EthFeeHistory")
	as.AliasMethod("eth_protocolVersion", "Filecoin.EthProtocolVersion")
	as.AliasMethod("eth_maxPriorityFeePerGas", "Filecoin.EthMaxPriorityFeePerGas")
	as.AliasMethod("eth_gasPrice", "Filecoin.EthGasPrice")
	as.AliasMethod("eth_sendRawTransaction", "Filecoin.EthSendRawTransaction")
	as.AliasMethod("eth_estimateGas", "Filecoin.EthEstimateGas")
	as.AliasMethod("eth_call", "Filecoin.EthCall")

	as.AliasMethod("eth_getLogs", "Filecoin.EthGetLogs")
	as.AliasMethod("eth_getFilterChanges", "Filecoin.EthGetFilterChanges")
	as.AliasMethod("eth_getFilterLogs", "Filecoin.EthGetFilterLogs")
	as.AliasMethod("eth_newFilter", "Filecoin.EthNewFilter")
	as.AliasMethod("eth_newBlockFilter", "Filecoin.EthNewBlockFilter")
	as.AliasMethod("eth_newPendingTransactionFilter", "Filecoin.EthNewPendingTransactionFilter")
	as.AliasMethod("eth_uninstallFilter", "Filecoin.EthUninstallFilter")
	as.AliasMethod("eth_subscribe", "Filecoin.EthSubscribe")
	as.AliasMethod("eth_unsubscribe", "Filecoin.EthUnsubscribe")

	as.AliasMethod("trace_block", "Filecoin.EthTraceBlock")
	as.AliasMethod("trace_replayBlockTransactions", "Filecoin.EthTraceReplayBlockTransactions")

	as.AliasMethod("net_version", "Filecoin.NetVersion")
	as.AliasMethod("net_listening", "Filecoin.NetListening")

	as.AliasMethod("web3_clientVersion", "Filecoin.Web3ClientVersion")
}
