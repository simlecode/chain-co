package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/filecoin-project/go-jsonrpc"
	local_api "github.com/ipfs-force-community/sophon-co/cli/api"
	"github.com/ipfs-force-community/sophon-co/config"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
)

func NewLocalRPCClient(cctx *cli.Context, opts ...jsonrpc.Option) (local_api.LocalAPI, jsonrpc.ClientCloser, error) {
	repoPath, err := homedir.Expand(cctx.String("repo"))
	if err != nil {
		return nil, nil, err
	}
	cfg, err := config.ReadConfig(filepath.Join(repoPath, config.ConfigFile))
	if err != nil {
		return nil, nil, err
	}

	addr := cfg.API.ListenAddress
	if cctx.IsSet("listen") {
		addr = cctx.String("listen")
	}
	port := strings.Split(addr, ":")[1]
	endpoint := fmt.Sprintf("http://127.0.0.1:%s/rpc/admin/v0", port)

	token, err := os.ReadFile(filepath.Join(repoPath, config.TokenFile))
	token = bytes.TrimSpace(token)
	if err != nil {
		return nil, nil, err
	}

	headers := http.Header{}
	headers.Add("Authorization", "Bearer "+string(token))

	var res local_api.LocalAPIStruct
	closer, err := jsonrpc.NewMergeClient(cctx.Context, endpoint, "Filecoin",
		[]interface{}{
			&res,
		},
		headers,
		opts...,
	)

	return &res, closer, err
}
