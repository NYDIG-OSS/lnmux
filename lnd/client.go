package lnd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/chainrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/bottlepay/lnmux/common"
)

//go:generate mockgen --destination=mock_lnd_client.go --self_package=github.com/bottlepay/lnmux/lnd --package=lnd github.com/bottlepay/lnmux/lnd LndClient

var ErrInterceptorNotRequired = errors.New("lnd requireinterceptor flag not set")

type LndClient interface {
	PubKey() common.PubKey
	Network() *chaincfg.Params

	RegisterBlockEpochNtfn(ctx context.Context) (chan *chainrpc.BlockEpoch,
		chan error, error)

	HtlcInterceptor(ctx context.Context) (
		func(*routerrpc.ForwardHtlcInterceptResponse) error,
		func() (*routerrpc.ForwardHtlcInterceptRequest, error),
		error)
}

type lndClient struct {
	cfg    Config
	logger *zap.SugaredLogger

	grpcClient     *grpc.ClientConn
	lnClient       lnrpc.LightningClient
	routerClient   routerrpc.RouterClient
	notifierClient chainrpc.ChainNotifierClient
}

type Config struct {
	// Connection
	TlsCertPath  string
	MacaroonPath string
	LndUrl       string
	Network      *chaincfg.Params
	PubKey       common.PubKey
	Timeout      time.Duration

	Logger *zap.SugaredLogger
}

func NewLndClient(cfg Config) (LndClient, error) {
	logger := cfg.Logger.With("node", cfg.PubKey.String())

	// load grpc connection with config properties
	conn, err := loadGrpcConn(cfg.TlsCertPath, cfg.MacaroonPath, cfg.LndUrl)
	if err != nil {
		return nil, err
	}

	client := &lndClient{
		cfg:    cfg,
		logger: logger,

		grpcClient:     conn,
		lnClient:       lnrpc.NewLightningClient(conn),
		routerClient:   routerrpc.NewRouterClient(conn),
		notifierClient: chainrpc.NewChainNotifierClient(conn),
	}

	// Test the lnd connection if it is available.
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	getInfoResp, err := client.lnClient.GetInfo(ctx, &lnrpc.GetInfoRequest{})
	if err != nil {
		logger.Warnw("Node unavailable, skipping parameter check",
			"err", err)

		return client, nil
	}

	// Verify chain.
	lndNetwork := getInfoResp.Chains[0].Network
	networkStr := cfg.Network.Name
	if networkStr == "testnet3" {
		networkStr = "testnet"
	}
	if lndNetwork != networkStr {
		return nil, fmt.Errorf("unexpected network: expected %v, connected to %v",
			networkStr, lndNetwork)
	}

	// Verify pubkey.
	pubKey, err := common.NewPubKeyFromStr(getInfoResp.IdentityPubkey)
	if err != nil {
		return nil, fmt.Errorf("invalid identity pubkey: %w", err)
	}
	if pubKey != cfg.PubKey {
		return nil, fmt.Errorf("unexpected pubkey: expected %v, connected to %v",
			cfg.PubKey, pubKey)
	}

	logger.Infow("Succesfully connected to LND")

	return client, nil
}

func (l *lndClient) PubKey() common.PubKey {
	return l.cfg.PubKey
}

func (l *lndClient) Network() *chaincfg.Params {
	return l.cfg.Network
}

func (l *lndClient) RegisterBlockEpochNtfn(ctx context.Context) (
	chan *chainrpc.BlockEpoch, chan error, error) {

	stream, err := l.notifierClient.RegisterBlockEpochNtfn(
		ctx, &chainrpc.BlockEpoch{},
	)
	if err != nil {
		return nil, nil, err
	}

	var (
		errChan   = make(chan error)
		blockChan = make(chan *chainrpc.BlockEpoch)
	)
	go func() {
		for {
			event, err := stream.Recv()
			if err != nil {
				errChan <- err
				return
			}

			blockChan <- event
		}
	}()

	return blockChan, errChan, nil
}

func (l *lndClient) HtlcInterceptor(ctx context.Context) (
	func(*routerrpc.ForwardHtlcInterceptResponse) error,
	func() (*routerrpc.ForwardHtlcInterceptRequest, error),
	error) {

	infoCtx, cancel := context.WithTimeout(ctx, l.cfg.Timeout)
	defer cancel()

	infoResp, err := l.lnClient.GetInfo(infoCtx, &lnrpc.GetInfoRequest{})
	if err != nil {
		return nil, nil, err
	}

	// Check to see if lnd is running with --requireinterceptor. Otherwise usage
	// of the HTLC interceptor API is unsafe.
	if !infoResp.RequireHtlcInterceptor {
		return nil, nil, ErrInterceptorNotRequired
	}

	stream, err := l.routerClient.HtlcInterceptor(ctx)
	if err != nil {
		return nil, nil, err
	}

	return stream.Send, stream.Recv, nil
}
