package lnd

import (
	"context"
	"errors"
	"fmt"

	"github.com/bottlepay/lnmux/common"
	"github.com/bottlepay/lnmux/types"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/chainrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

	HtlcNotifier(ctx context.Context) (
		func() (*routerrpc.HtlcEvent, error), error)

	LookupHtlc(ctx context.Context, key types.CircuitKey) (bool, error)
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

	Logger *zap.SugaredLogger
}

func NewLndClient(ctx context.Context, cfg Config) (LndClient, error) {
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

	logger.Infow("Successfully connected to LND")

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

	infoResp, err := l.lnClient.GetInfo(ctx, &lnrpc.GetInfoRequest{})
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

func (l *lndClient) HtlcNotifier(ctx context.Context) (
	func() (*routerrpc.HtlcEvent, error), error) {

	stream, err := l.routerClient.SubscribeHtlcEvents(
		ctx, &routerrpc.SubscribeHtlcEventsRequest{},
	)

	// TODO: Move this err mapping to a helper?
	switch status.Code(err) {
	case codes.OK:

	case codes.DeadlineExceeded:
		return nil, context.DeadlineExceeded

	case codes.Canceled:
		return nil, context.Canceled

	default:
		return nil, err
	}

	recv := func() (*routerrpc.HtlcEvent, error) {
		event, err := stream.Recv()
		switch status.Code(err) {
		case codes.OK:

		case codes.DeadlineExceeded:
			return nil, context.DeadlineExceeded

		case codes.Canceled:
			return nil, context.Canceled

		default:
			return nil, err
		}

		return event, nil
	}

	// Wait for SubscribedEvent which signals that the stream has been
	// established. This is important to prevent race conditions with
	// LookupHtlc.
	firstEvent, err := recv()
	if err != nil {
		return nil, err
	}

	_, ok := firstEvent.Event.(*routerrpc.HtlcEvent_SubscribedEvent)
	if !ok {
		return nil, errors.New("stream did not start with subscribed event")
	}

	return recv, nil
}

func (l *lndClient) LookupHtlc(ctx context.Context, key types.CircuitKey) (
	bool, error) {

	resp, err := l.lnClient.LookupHtlc(ctx, &lnrpc.LookupHtlcRequest{
		ChanId:    key.ChanID,
		HtlcIndex: key.HtlcID,
	})
	if err != nil {
		return false, err
	}

	return resp.Settled, nil
}
