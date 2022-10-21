package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bottlepay/lnmux"
	"github.com/bottlepay/lnmux/common"
	"github.com/bottlepay/lnmux/dlock"
	"github.com/bottlepay/lnmux/lnd"
	"github.com/bottlepay/lnmux/lnmuxrpc"
	"github.com/bottlepay/lnmux/persistence"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/go-pg/pg/v10"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_zap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/lightningnetwork/lnd/clock"
	"github.com/lightningnetwork/lnd/keychain"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/urfave/cli/v2"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var runCommand = &cli.Command{
	Name:   "run",
	Action: runAction,
}

func runAction(c *cli.Context) error {
	cfg, err := loadConfig(c)
	if err != nil {
		return err
	}

	err = initLogger(cfg.Logging.Level, cfg.Logging.Format, cfg.Logging.WithCaller)
	if err != nil {
		return err
	}

	return initServiceWithLock(cfg.InstrumentationAddress, cfg.DistributedLock, func(ctx context.Context) error {
		return run(ctx, cfg)
	})
}

func initServiceWithLock(address string, lockConfig DistributedLockConfig, run func(context.Context) error) error {
	group, ctx := errgroup.WithContext(context.Background())

	group.Go(func() error {
		log.Infof("Press ctrl-c to exit")

		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

		select {
		case <-sigint:
			return errors.New("user requested termination")

		case <-ctx.Done():
			return nil
		}
	})

	instServer := initInstrumentationServer(address)

	group.Go(func() error {
		log.Infow("Instrumentation HTTP server starting",
			"instrumentationAddress", instServer.Addr)

		return instServer.ListenAndServe()
	})

	group.Go(func() error {
		<-ctx.Done()

		// Stop instrumentation server
		log.Infow("Instrumentation server stopping")

		return instServer.Close()
	})

	group.Go(func() error {
		releaseLock, err := initDistributedLock(ctx, &lockConfig)
		if err != nil {
			return err
		}
		defer releaseLock()

		return run(ctx)
	})

	return group.Wait()
}

func run(ctx context.Context, cfg *Config) error {
	// Setup persistence.
	db, err := initPersistence(ctx, cfg)
	if err != nil {
		return err
	}

	// Parse lnd connection info from the configuration.
	lnds, activeNetParams, err := initLndClients(ctx, &cfg.Lnd)
	if err != nil {
		return err
	}

	// Get identity key so that incoming htlcs can be decoded.
	identityKey, err := cfg.GetIdentityKey()
	if err != nil {
		return err
	}

	keyRing := lnmux.NewKeyRing(identityKey)

	// Log identity key.
	pubKey, _ := keyRing.DeriveKey(keychain.KeyLocator{})
	keyBytes := pubKey.PubKey.SerializeCompressed()
	key, _ := common.NewPubKeyFromBytes(keyBytes)
	log.Infow("Mux starting",
		"key", key,
		"network", activeNetParams.Name)

	// Get routing policy.
	routingPolicy := cfg.GetRoutingPolicy()

	// Get a new creator instance.
	var gwPubKeys []common.PubKey
	for _, lnd := range lnds {
		gwPubKeys = append(gwPubKeys, lnd.PubKey())
	}
	creator, err := lnmux.NewInvoiceCreator(
		&lnmux.InvoiceCreatorConfig{
			KeyRing:         keyRing,
			GwPubKeys:       gwPubKeys,
			ActiveNetParams: activeNetParams,
			RoutingPolicy:   routingPolicy,
		},
	)
	if err != nil {
		return err
	}

	// Instantiate multiplexer.
	registry := lnmux.NewRegistry(
		db,
		&lnmux.RegistryConfig{
			Clock:                         clock.NewDefaultClock(),
			FinalCltvRejectDelta:          10,
			HtlcHoldDuration:              30 * time.Second,
			AcceptTimeout:                 60 * time.Second,
			Logger:                        log,
			PrivKey:                       identityKey,
			AutoSettle:                    cfg.AutoSettle,
			GracePeriodWithoutSubscribers: lnmux.DefaultGracePeriodWithoutSubscribers,
		},
	)

	settledHandler := lnmux.NewSettledHandler(
		&lnmux.SettledHandlerConfig{
			Logger:    log,
			Persister: db,
		},
	)

	mux, err := lnmux.New(
		&lnmux.MuxConfig{
			KeyRing:         keyRing,
			ActiveNetParams: activeNetParams,
			Lnd:             lnds,
			Logger:          log,
			Registry:        registry,
			SettledHandler:  settledHandler,
			RoutingPolicy:   routingPolicy,
		})
	if err != nil {
		return err
	}

	// Instantiate grpc server and enable reflection and Prometheus metrics.
	streamInterceptors := []grpc.StreamServerInterceptor{
		grpc_prometheus.StreamServerInterceptor,
	}

	unaryInterceptors := []grpc.UnaryServerInterceptor{
		grpc_prometheus.UnaryServerInterceptor,
	}

	if cfg.Logging.GrpcLogging {
		unaryInterceptors = append(unaryInterceptors,
			grpc_zap.UnaryServerInterceptor(log.Desugar()),
		)
		streamInterceptors = append(streamInterceptors,
			grpc_zap.StreamServerInterceptor(log.Desugar()), //nolint: contextcheck
		)
	}

	if cfg.Logging.GrpcPayloadLogging {
		decider := func(ctx context.Context, fullMethodName string,
			servingObject interface{}) bool {

			// Log everything.
			return true
		}

		unaryInterceptors = append(unaryInterceptors,
			grpc_zap.PayloadUnaryServerInterceptor(log.Desugar(), decider),
		)
		streamInterceptors = append(streamInterceptors,
			grpc_zap.PayloadStreamServerInterceptor(log.Desugar(), decider), //nolint: contextcheck
		)
	}

	grpcServer := grpc.NewServer(
		grpc.StreamInterceptor(grpc_middleware.ChainStreamServer(
			streamInterceptors...,
		)),
		grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(
			unaryInterceptors...,
		)),
	)

	reflection.Register(grpcServer)
	grpc_prometheus.Register(grpcServer)

	server := newServer(creator, registry, settledHandler)

	lnmuxrpc.RegisterServiceServer(
		grpcServer, server,
	)

	listenAddress := cfg.ListenAddress
	if listenAddress == "" {
		listenAddress = DefaultListenAddress
	}

	grpcInternalListener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return err
	}

	group, ctx := errgroup.WithContext(ctx)

	// Run multiplexer.
	group.Go(func() error {
		err := mux.Run(ctx)
		if err != nil {
			log.Errorw("mux error", "err", err)
		}

		return err
	})

	// Run grpc server.
	group.Go(func() error {
		log.Infow("Grpc server starting", "listenAddress", listenAddress)
		err := grpcServer.Serve(grpcInternalListener)
		if err != nil && err != grpc.ErrServerStopped {
			log.Errorw("grpc server error", "err", err)
		}

		return err
	})

	group.Go(func() error {
		<-ctx.Done()

		// Stop grpc server.
		log.Infof("Stopping grpc server")
		grpcServer.Stop()

		return nil
	})

	return group.Wait()
}

func initLndClients(ctx context.Context, cfg *LndConfig) ([]lnd.LndClient, *chaincfg.Params, error) {
	var (
		nodes []lnd.LndClient
	)

	network, err := network(cfg.Network)
	if err != nil {
		return nil, nil, err
	}

	seenPubKeys := make(map[common.PubKey]struct{})
	for _, node := range cfg.Nodes {
		pubkey, err := common.NewPubKeyFromStr(node.PubKey)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot parse pubkey %v: %v", node.PubKey, err)
		}

		lnd, err := lnd.NewLndClient(ctx, lnd.Config{
			TlsCertPath:  node.TlsCertPath,
			MacaroonPath: node.MacaroonPath,
			LndUrl:       node.LndUrl,
			Logger:       log,
			PubKey:       pubkey,
			Network:      network,
			Timeout:      cfg.Timeout,
		})
		if err != nil {
			return nil, nil, err
		}
		pubKey := lnd.PubKey()
		if _, exists := seenPubKeys[pubKey]; exists {
			return nil, nil, fmt.Errorf("duplicate lnd node: %v", pubKey)
		}
		seenPubKeys[pubKey] = struct{}{}

		nodes = append(nodes, lnd)
	}

	return nodes, network, nil
}

func network(network string) (*chaincfg.Params, error) {
	switch network {
	case chaincfg.MainNetParams.Name:
		return &chaincfg.MainNetParams, nil
	case chaincfg.TestNet3Params.Name:
	case "testnet":
		return &chaincfg.TestNet3Params, nil
	case chaincfg.RegressionNetParams.Name:
		return &chaincfg.RegressionNetParams, nil
	case chaincfg.SimNetParams.Name:
		return &chaincfg.SimNetParams, nil
	}

	return nil, fmt.Errorf("unsupported network %v", network)
}

func initPersistence(ctx context.Context, cfg *Config) (*persistence.PostgresPersister, error) {
	options, err := pg.ParseURL(cfg.DB.DSN)
	if err != nil {
		return nil, err
	}

	// Apply connection options
	options.PoolSize = cfg.DB.PoolSize
	options.MinIdleConns = cfg.DB.MinIdleConns
	options.MaxConnAge = cfg.DB.MaxConnAge
	options.PoolTimeout = cfg.DB.PoolTimeout
	options.IdleTimeout = cfg.DB.IdleTimeout

	// Setup persistence
	db := persistence.NewPostgresPersisterFromOptions(options, log) //nolint: contextcheck
	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()

	// Ensure we can reach the server
	if err := db.Ping(ctx); err != nil {
		return nil, err
	}

	return db, nil
}

func initDistributedLock(ctx context.Context, cfg *DistributedLockConfig) (func(), error) {
	// Do nothing if no lock name is specified.
	if cfg.Name == "" {
		log.Infow("Not using leader election")

		return func() {}, nil
	}

	return dlock.New(ctx, &dlock.LockConfig{
		Namespace:     cfg.Namespace,
		Name:          cfg.Name,
		ID:            cfg.ID,
		DevKubeConfig: cfg.DevKubeConfig,
		Logger:        log,
	})
}

func initInstrumentationServer(instAddress string) *http.Server {
	if instAddress == "" {
		instAddress = DefaultInstrumentationAddress
	}

	// Instantiate a new HTTP server and mux.
	instMux := http.NewServeMux()

	// Register the Prometheus handler.
	instMux.Handle("/metrics", promhttp.Handler())

	// Register the pprof handlers. We do this manually because we aren't
	// using the default mux.
	// See issues https://github.com/golang/go/issues/42834 and
	// https://github.com/golang/go/issues/22085
	instMux.HandleFunc("/debug/pprof", pprof.Index)
	instMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	instMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	instMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	instMux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	instMux.Handle("/debug/pprof/allocs", pprof.Handler("allocs"))
	instMux.Handle("/debug/pprof/block", pprof.Handler("block"))
	instMux.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	instMux.Handle("/debug/pprof/heap", pprof.Handler("heap"))
	instMux.Handle("/debug/pprof/mutex", pprof.Handler("mutex"))
	instMux.Handle("/debug/pprof/threadcreate", pprof.Handler("threadcreate"))

	return &http.Server{
		Addr:    instAddress,
		Handler: instMux,

		// Even though this server should only be exposed to trusted
		// clients, this mitigates slowloris-like DoS attacks.
		ReadHeaderTimeout: 10 * time.Second,
	}
}
