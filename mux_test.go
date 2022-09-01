package lnmux

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bottlepay/lnmux/common"
	"github.com/bottlepay/lnmux/lnd"
	"github.com/bottlepay/lnmux/persistence"
	"github.com/bottlepay/lnmux/persistence/test"
	test_common "github.com/bottlepay/lnmux/test"
	"github.com/bottlepay/lnmux/types"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/golang/mock/gomock"
	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/clock"
	"github.com/lightningnetwork/lnd/lnrpc/chainrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/record"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

var (
	testPubKey1, _ = common.NewPubKeyFromStr("02e1ce77dfdda9fd1cf5e9d796faf57d1cedef9803aec84a6d7f8487d32781341e")
	testPubKey2, _ = common.NewPubKeyFromStr("0314aaf9b2547682b81977b3ac0c5585c3521a0a5430fb410cb572d5c72364edf3")

	testKey = [32]byte{
		0x81, 0xb6, 0x37, 0xd8, 0xfc, 0xd2, 0xc6, 0xda,
		0x68, 0x59, 0xe6, 0x96, 0x31, 0x13, 0xa1, 0x17,
		0xd, 0xe7, 0x93, 0xe4, 0xb7, 0x25, 0xb8, 0x4d,
		0x1e, 0xb, 0x4c, 0xf9, 0x9e, 0xc5, 0x8c, 0xe9,
	}
)

type testLndClient struct {
	client       *lnd.MockLndClient
	htlcChan     chan *routerrpc.ForwardHtlcInterceptRequest
	responseChan chan *routerrpc.ForwardHtlcInterceptResponse
	blockChan    chan *chainrpc.BlockEpoch
	closeChan    chan struct{}
}

func createTestLndClient(ctrl *gomock.Controller,
	pubKey common.PubKey) *testLndClient {

	lndClient := lnd.NewMockLndClient(ctrl)
	lndClient.EXPECT().PubKey().Return(pubKey).AnyTimes()

	htlcChan := make(chan *routerrpc.ForwardHtlcInterceptRequest)
	responseChan := make(chan *routerrpc.ForwardHtlcInterceptResponse, 1)
	closeChan := make(chan struct{})
	lndClient.EXPECT().HtlcInterceptor(gomock.Any()).
		DoAndReturn(func(ctx context.Context) (
			func(*routerrpc.ForwardHtlcInterceptResponse) error,
			func() (*routerrpc.ForwardHtlcInterceptRequest, error),
			error) {

			return func(response *routerrpc.ForwardHtlcInterceptResponse) error {
					responseChan <- response

					return nil
				},
				func() (*routerrpc.ForwardHtlcInterceptRequest, error) {
					select {
					case htlc := <-htlcChan:
						return htlc, nil

					case <-ctx.Done():
						return nil, ctx.Err()

					case <-closeChan:
						return nil, errors.New("connection closed")
					}
				},
				nil
		}).AnyTimes()

	blockChan := make(chan *chainrpc.BlockEpoch)
	lndClient.EXPECT().RegisterBlockEpochNtfn(gomock.Any()).
		Return(blockChan, nil, nil).AnyTimes()

	return &testLndClient{
		client:       lndClient,
		htlcChan:     htlcChan,
		responseChan: responseChan,
		blockChan:    blockChan,
		closeChan:    closeChan,
	}
}

func TestMux(t *testing.T) {
	defer test_common.Timeout()()
	t.Parallel()

	logger, _ := zap.NewDevelopment()

	keyRing := NewKeyRing(testKey)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	l := []*testLndClient{
		createTestLndClient(ctrl, testPubKey1),
		createTestLndClient(ctrl, testPubKey2),
	}

	activeNetParams := &chaincfg.RegressionNetParams

	db, dropDB := setupTestDB(t)
	defer dropDB()

	creator, err := NewInvoiceCreator(
		&InvoiceCreatorConfig{
			KeyRing:         keyRing,
			GwPubKeys:       []common.PubKey{testPubKey1, testPubKey2},
			ActiveNetParams: activeNetParams,
		},
	)
	require.NoError(t, err)

	invoice, testPreimage, err := creator.Create(
		10000, time.Minute, "test", nil, 40,
	)
	require.NoError(t, err)
	require.NotNil(t, invoice)

	testHash := testPreimage.Hash()

	registry := NewRegistry(
		db,
		&RegistryConfig{
			Clock:                clock.NewDefaultClock(),
			FinalCltvRejectDelta: 10,
			HtlcHoldDuration:     time.Second,
			AcceptTimeout:        time.Second * 5,
			Logger:               logger.Sugar(),
			PrivKey:              testKey,
		},
	)

	settledHandler := NewSettledHandler(&SettledHandlerConfig{
		Persister: db,
		Logger:    logger.Sugar(),
	})

	mux, err := New(&MuxConfig{
		KeyRing:         keyRing,
		ActiveNetParams: activeNetParams,
		Lnd:             []lnd.LndClient{l[0].client, l[1].client},
		Logger:          logger.Sugar(),
		Registry:        registry,
		SettledHandler:  settledHandler,
	})
	require.NoError(t, err)

	errChan := make(chan error)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		errChan <- mux.Run(ctx)
	}()

	var acceptChan = make(chan acceptEvent, 1)
	cancelAcceptSubscription, err := registry.SubscribeAccept(
		func(hash lntypes.Hash, setID SetID) {
			logger.Sugar().Infow("Payment accepted",
				"hash", hash, "setID", setID)

			acceptChan <- acceptEvent{hash: hash, setID: setID}
		},
	)
	require.NoError(t, err)

	expectAccept := func(expectedHash lntypes.Hash) SetID {
		t.Helper()

		accept := <-acceptChan
		require.Equal(t, expectedHash, accept.hash)

		return accept.setID
	}

	// Send initial block heights.
	l[0].blockChan <- &chainrpc.BlockEpoch{Height: 1000}
	l[1].blockChan <- &chainrpc.BlockEpoch{Height: 1000}

	// Generate data for test htlc.
	dest, err := route.NewVertexFromBytes(keyRing.pubKey.SerializeCompressed())
	require.NoError(t, err)

	genOnion := func() []byte {
		route := &route.Route{
			Hops: []*route.Hop{
				{
					PubKeyBytes: dest,
					MPP: record.NewMPP(
						invoice.Value, invoice.PaymentAddr,
					),
				},
			},
		}

		sessionKey, err := btcec.NewPrivateKey()
		require.NoError(t, err)

		onionBlob, err := generateSphinxPacket(route, testHash[:], sessionKey)
		require.NoError(t, err)

		return onionBlob
	}

	onionBlob := genOnion()

	receiveHtlc := func(sourceNodeIdx int, htlcID uint64,
		amt int64) {

		virtualChannel := virtualChannelFromNode(
			l[sourceNodeIdx].client.PubKey(),
		)

		l[sourceNodeIdx].htlcChan <- &routerrpc.ForwardHtlcInterceptRequest{
			IncomingCircuitKey:      &routerrpc.CircuitKey{HtlcId: htlcID},
			PaymentHash:             testHash[:],
			IncomingExpiry:          1050,
			OutgoingAmountMsat:      uint64(amt),
			OutgoingExpiry:          1040,
			OnionBlob:               onionBlob[:],
			OutgoingRequestedChanId: virtualChannel,
		}
	}

	expectResponse := func(resp *routerrpc.ForwardHtlcInterceptResponse,
		htlcID int, expectedAction routerrpc.ResolveHoldForwardAction) {

		t.Helper()

		require.Equal(t, uint64(htlcID), resp.IncomingCircuitKey.HtlcId)
		require.Equal(t, expectedAction, resp.Action)
	}

	// Notify arrival of part 1.
	// db.SettleErr = nil
	receiveHtlc(0, 0, 6000)

	// Replay arrival of part 1.
	receiveHtlc(0, 0, 6000)

	// Let it time out. Expect two responses, one for each notified arrival.
	expectResponse(<-l[0].responseChan, 0, routerrpc.ResolveHoldForwardAction_FAIL)
	expectResponse(<-l[0].responseChan, 0, routerrpc.ResolveHoldForwardAction_FAIL)

	// Notify arrival of part 1.
	receiveHtlc(0, 1, 6000)

	// Notify arrival of part 2.
	receiveHtlc(1, 2, 4000)

	setID := expectAccept(testHash)

	// No settle requested yet and we expect WaitForInvoiceSettled to return an
	// error.
	require.ErrorIs(t,
		settledHandler.WaitForInvoiceSettled(context.Background(), testHash),
		types.ErrInvoiceNotFound)

	// Disconnect htlc interceptor.
	l[0].closeChan <- struct{}{}

	// Wait for disconnect to be processed.
	time.Sleep(time.Second)

	// Request settle, even though node 1 is still offline.
	require.NoError(t, registry.RequestSettle(testHash, setID))

	// Expect a settle signal for node 2.
	expectResponse(<-l[1].responseChan, 2, routerrpc.ResolveHoldForwardAction_SETTLE)

	// Wait for invoice-level settle signal.
	settledChan := make(chan struct{})
	go func() {
		assert.NoError(t,
			settledHandler.WaitForInvoiceSettled(
				context.Background(), testHash,
			))

		close(settledChan)
	}()

	// Resend block height and htlc to simulate the interceptor coming back
	// online.
	l[0].blockChan <- &chainrpc.BlockEpoch{Height: 1000}
	receiveHtlc(0, 1, 6000)

	// Expect the other htlc to be settled on node 1 now.
	expectResponse(<-l[0].responseChan, 1, routerrpc.ResolveHoldForwardAction_SETTLE)

	// Also the settle event is expected now.
	<-settledChan

	// Waiting for settle again should return immediately with success.
	require.NoError(t, settledHandler.WaitForInvoiceSettled(
		context.Background(), testHash,
	))

	_, htlcs, err := db.Get(context.Background(), testHash)
	require.NoError(t, err)
	require.NotEmpty(t, htlcs)

	// Replay settled htlc.
	receiveHtlc(0, 1, 6000)
	expectResponse(<-l[0].responseChan, 1, routerrpc.ResolveHoldForwardAction_SETTLE)

	// New payment to settled invoice
	receiveHtlc(0, 10, 10000)
	expectResponse(<-l[0].responseChan, 10, routerrpc.ResolveHoldForwardAction_FAIL)

	// Create a new invoice.
	invoice, testPreimage, err = creator.Create(
		15000, time.Minute, "test 2", nil, 40,
	)
	require.NoError(t, err)
	require.NotNil(t, invoice)

	testHash = testPreimage.Hash()

	// Regenerate onion blob for new hash.
	onionBlob = genOnion()

	receiveHtlc(0, 20, 15000)

	setID = expectAccept(testHash)
	require.NoError(t, registry.RequestSettle(testHash, setID))

	expectResponse(<-l[0].responseChan, 20, routerrpc.ResolveHoldForwardAction_SETTLE)

	cancelAcceptSubscription()

	cancel()
	require.NoError(t, <-errChan)
}

// generateSphinxPacket generates then encodes a sphinx packet which encodes
// the onion route specified by the passed layer 3 route. The blob returned
// from this function can immediately be included within an HTLC add packet to
// be sent to the first hop within the route.
func generateSphinxPacket(rt *route.Route, paymentHash []byte,
	sessionKey *btcec.PrivateKey) ([]byte, error) {

	// Now that we know we have an actual route, we'll map the route into a
	// sphinx payument path which includes per-hop paylods for each hop
	// that give each node within the route the necessary information
	// (fees, CLTV value, etc) to properly forward the payment.
	sphinxPath, err := rt.ToSphinxPath()
	if err != nil {
		return nil, err
	}

	// Next generate the onion routing packet which allows us to perform
	// privacy preserving source routing across the network.
	sphinxPacket, err := sphinx.NewOnionPacket(
		sphinxPath, sessionKey, paymentHash,
		sphinx.DeterministicPacketFiller,
	)
	if err != nil {
		return nil, err
	}

	// Finally, encode Sphinx packet using its wire representation to be
	// included within the HTLC add packet.
	var onionBlob bytes.Buffer
	if err := sphinxPacket.Encode(&onionBlob); err != nil {
		return nil, err
	}

	return onionBlob.Bytes(), nil
}

func setupTestDB(t *testing.T) (*persistence.PostgresPersister, func()) {
	dbSettings := test.CreatePGTestDB(t, &test.TestDBSettings{
		MigrationsPath: "./persistence/migrations",
	})

	log := zap.NewNop().Sugar()
	db := persistence.NewPostgresPersisterFromOptions(dbSettings, log)

	drop := func() {
		db.Close()
		test.DropTestDB(t, *dbSettings)
	}

	return db, drop
}
