package lnd

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/bottlepay/lnmux/common"
	"github.com/bottlepay/lnmux/types"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/hashicorp/go-version"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/chainrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lnrpc/verrpc"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

//go:generate mockgen --destination=mock_lnd_client.go --self_package=github.com/bottlepay/lnmux/lnd --package=lnd github.com/bottlepay/lnmux/lnd LndClient

var (
	ErrInterceptorNotRequired = errors.New("lnd requireinterceptor flag not set")

	ErrFinalHtlcResolutionsNotStored = errors.New("lnd store-final-htlc-resolutions flag not set")

	ErrHtlcNotFound = errors.New("htlc not found")
)

var minRequiredLndVersion, _ = version.NewSemver("v0.16.0-beta.rc2")

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
	verClient      verrpc.VersionerClient
	routerClient   routerrpc.RouterClient
	notifierClient chainrpc.ChainNotifierClient

	configCheckPassed bool
	configCheckLock   sync.Mutex
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
		verClient:      verrpc.NewVersionerClient(conn),
	}

	// Test the lnd connection if it is available.
	if err := client.tryValidateConfig(ctx); err != nil {
		logger.Warnw("Node unavailable or misconfigured",
			"err", err)
	}

	return client, nil
}

func (l *lndClient) tryValidateConfig(ctx context.Context) error {
	// Obtain lock because validation can be triggered from multiple goroutines.
	l.configCheckLock.Lock()
	defer l.configCheckLock.Unlock()

	// Only validate the config once.
	if l.configCheckPassed {
		return nil
	}

	// Request version info.
	ver, err := l.verClient.GetVersion(ctx, &verrpc.VersionRequest{})
	if err != nil {
		return err
	}

	// Verify version against minimum requirement.
	lndVersion, err := version.NewSemver(ver.Version)
	if err != nil {
		return err
	}

	if !lndVersion.GreaterThanOrEqual(minRequiredLndVersion) {
		return fmt.Errorf("connected to lnd version %v, "+
			"but minimum required version is %v",
			lndVersion, minRequiredLndVersion)
	}

	// Request node info.
	info, err := l.lnClient.GetInfo(ctx, &lnrpc.GetInfoRequest{})
	if err != nil {
		return err
	}

	// Verify chain.
	lndNetwork := info.Chains[0].Network
	networkStr := l.cfg.Network.Name
	if networkStr == "testnet3" {
		networkStr = "testnet"
	}
	if lndNetwork != networkStr {
		return fmt.Errorf("unexpected network: expected %v, connected to %v",
			networkStr, lndNetwork)
	}

	// Verify pubkey.
	pubKey, err := common.NewPubKeyFromStr(info.IdentityPubkey)
	if err != nil {
		return fmt.Errorf("invalid identity pubkey: %w", err)
	}
	if pubKey != l.cfg.PubKey {
		return fmt.Errorf("unexpected pubkey: expected %v, connected to %v",
			l.cfg.PubKey, pubKey)
	}

	// Check to see if lnd is running with --requireinterceptor. Otherwise usage
	// of the HTLC interceptor API is unsafe.
	if !info.RequireHtlcInterceptor {
		return ErrInterceptorNotRequired
	}

	// Check to see if lnd is running with --store-final-htlc-resolutions.
	// Otherwise we can't obtain final resolution information and invoices will
	// never be marked as settled.
	if !info.StoreFinalHtlcResolutions {
		return ErrFinalHtlcResolutionsNotStored
	}

	// Set flag to prevent checking again.
	l.configCheckPassed = true

	return nil
}

func (l *lndClient) PubKey() common.PubKey {
	return l.cfg.PubKey
}

func (l *lndClient) Network() *chaincfg.Params {
	return l.cfg.Network
}

func (l *lndClient) RegisterBlockEpochNtfn(ctx context.Context) (
	chan *chainrpc.BlockEpoch, chan error, error) {

	if err := l.tryValidateConfig(ctx); err != nil {
		return nil, nil, err
	}

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

	if err := l.tryValidateConfig(ctx); err != nil {
		return nil, nil, err
	}

	stream, err := l.routerClient.HtlcInterceptor(ctx)
	if err != nil {
		return nil, nil, err
	}

	return stream.Send, stream.Recv, nil
}

func mapGrpcError(err error) error {
	switch status.Code(err) {
	case codes.OK:
		return nil

	case codes.DeadlineExceeded:
		return context.DeadlineExceeded

	case codes.Canceled:
		return context.Canceled

	default:
		return err
	}
}

func (l *lndClient) HtlcNotifier(ctx context.Context) (
	func() (*routerrpc.HtlcEvent, error), error) {

	stream, err := l.routerClient.SubscribeHtlcEvents(
		ctx, &routerrpc.SubscribeHtlcEventsRequest{},
	)
	if err := mapGrpcError(err); err != nil {
		return nil, err
	}

	recv := func() (*routerrpc.HtlcEvent, error) {
		event, err := stream.Recv()
		if err := mapGrpcError(err); err != nil {
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

	resp, err := l.lnClient.LookupHtlcResolution(ctx,
		&lnrpc.LookupHtlcResolutionRequest{
			ChanId:    key.ChanID,
			HtlcIndex: key.HtlcID,
		},
	)
	switch {
	case status.Code(err) == codes.NotFound:
		return false, ErrHtlcNotFound

	case err != nil:
		return false, err
	}

	return resp.Settled, nil
}
