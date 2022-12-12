package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bottlepay/lnmux"
	"github.com/bottlepay/lnmux/common"
	"github.com/bottlepay/lnmux/lnd"
	"github.com/btcsuite/btcd/chaincfg"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_zap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
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

	return group.Wait()
}

func run(ctx context.Context, cfg *Config) error {
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

	// Instantiate multiplexers.
	mux, err := lnmux.New(
		&lnmux.MuxConfig{
			KeyRing:         keyRing,
			ActiveNetParams: activeNetParams,
			Lnd:             lnds,
			Logger:          log,
			SettledCallback: settledHandler.InvoiceSettled,
			RoutingPolicy:   routingPolicy,
		})
	if err != nil {
		return err
	}

	group, ctx := errgroup.WithContext(ctx)

	group.Go(func() error {
		err := mux.Run(ctx)
		if err != nil {
			log.Errorw("mux error", "err", err)
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

func initLndClients(ctx context.Context, cfg *LndConfig) (lnd.LndClient, *chaincfg.Params, error) {
	network, err := network(cfg.Network)
	if err != nil {
		return nil, nil, err
	}

	seenPubKeys := make(map[common.PubKey]struct{})

	pubkey, err := common.NewPubKeyFromStr(cfg.PubKey)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot parse pubkey %v: %v", cfg.PubKey, err)
	}

	lnd, err := lnd.NewLndClient(ctx, lnd.Config{
		TlsCertPath:  cfg.TlsCertPath,
		MacaroonPath: cfg.MacaroonPath,
		LndUrl:       cfg.LndUrl,
		Logger:       log,
		PubKey:       pubkey,
		Network:      network,
	})
	if err != nil {
		return nil, nil, err
	}
	pubKey := lnd.PubKey()
	if _, exists := seenPubKeys[pubKey]; exists {
		return nil, nil, fmt.Errorf("duplicate lnd node: %v", pubKey)
	}
	seenPubKeys[pubKey] = struct{}{}

	return lnd, network, nil
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
