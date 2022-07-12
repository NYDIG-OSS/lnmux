package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/bottlepay/lnmux"
	"github.com/bottlepay/lnmux/common"
	"github.com/bottlepay/lnmux/lnd"
	"github.com/bottlepay/lnmux/lnmuxrpc"
	"github.com/bottlepay/lnmux/persistence"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/go-pg/pg/v10"
	"github.com/lightningnetwork/lnd/clock"
	"github.com/urfave/cli/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var runCommand = &cli.Command{
	Name:   "run",
	Action: runAction,
}

func runAction(c *cli.Context) error {
	cfg, err := loadConfig(c.String("config"))
	if err != nil {
		return err
	}

	// Setup persistence.
	db, err := initPersistence(cfg)
	if err != nil {
		return err
	}

	// Parse lnd connection info from the configuration.
	lnds, activeNetParams, err := initLndClients(&cfg.Lnd)
	if err != nil {
		return err
	}

	// Get identity key so that incoming htlcs can be decoded.
	identityKey, err := cfg.GetIdentityKey()
	if err != nil {
		return err
	}

	keyRing := lnmux.NewKeyRing(identityKey)

	// Get a new creator instance.
	var gwPubKeys []common.PubKey
	for _, lnd := range lnds {
		gwPubKeys = append(gwPubKeys, lnd.PubKey())
	}
	creator, err := lnmux.NewInvoiceCreator(
		&lnmux.InvoiceCreatorConfig{
			KeyRing:         keyRing,
			GwPubKeys:       gwPubKeys,
			ActiveNetParams: &chaincfg.RegressionNetParams,
		},
	)
	if err != nil {
		return err
	}

	// Instantiate multiplexer.
	registry := lnmux.NewRegistry(
		db,
		&lnmux.RegistryConfig{
			Clock:                clock.NewDefaultClock(),
			FinalCltvRejectDelta: 10,
			HtlcHoldDuration:     30 * time.Second,
			AcceptTimeout:        60 * time.Second,
			Logger:               log,
			PrivKey:              identityKey,
			AutoSettle:           cfg.AutoSettle,
		},
	)

	mux, err := lnmux.New(
		&lnmux.MuxConfig{
			KeyRing:         keyRing,
			ActiveNetParams: activeNetParams,
			Lnd:             lnds,
			Logger:          log,
			Registry:        registry,
		})
	if err != nil {
		return err
	}

	var (
		wg             sync.WaitGroup
		processErrChan = make(chan error)
	)

	// Run multiplexer.
	muxCtx, muxCancel := context.WithCancel(context.Background())
	defer muxCancel()

	wg.Add(1)
	go func() {
		defer wg.Done()

		err := mux.Run(muxCtx)
		if err != nil {
			log.Errorw("mux error", "err", err)

			processErrChan <- err
		}
	}()

	// Start grpc server.
	grpcServer := grpc.NewServer()
	reflection.Register(grpcServer)

	server, err := newServer(creator, registry)
	if err != nil {
		return err
	}

	lnmuxrpc.RegisterServiceServer(
		grpcServer, server,
	)

	grpcInternalListener, err := net.Listen("tcp", ":19090")
	if err != nil {
		return err
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		log.Infow("Grpc server startin on port 19090")
		err := grpcServer.Serve(grpcInternalListener)
		if err != nil && err != grpc.ErrServerStopped {
			log.Errorw("grpc server error", "err", err)

			processErrChan <- err
		}
	}()

	// Wait for break and terminate.
	log.Infof("Press ctrl-c to exit")
	sigint := make(chan os.Signal, 1)
	signal.Notify(sigint, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	var processErr error
	select {
	case <-sigint:
	case processErr = <-processErrChan:
	}

	// Stop grpc server.
	log.Infof("Stopping grpc server")
	grpcServer.Stop()

	// Stop multiplexer.
	log.Infof("Stopping multiplexer")
	muxCancel()

	log.Infow("Waiting for goroutines to finish")
	wg.Wait()

	log.Infof("Exiting")

	return processErr
}

func initLndClients(cfg *LndConfig) ([]lnd.LndClient, *chaincfg.Params, error) {
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

		lnd, err := lnd.NewLndClient(lnd.Config{
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

func initPersistence(cfg *Config) (*persistence.PostgresPersister, error) {
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
	db := persistence.NewPostgresPersisterFromOptions(options, log)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	// Ensure we can reach the server
	if err := db.Ping(ctx); err != nil {
		return nil, err
	}

	return db, nil
}
