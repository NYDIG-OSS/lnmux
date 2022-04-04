package lnmux

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/golang/mock/gomock"
	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/lnrpc/chainrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/record"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bottlepay/lnmux/common"
	"github.com/bottlepay/lnmux/lnd"
)

var (
	testPubKey1, _ = common.NewPubKeyFromStr("02e1ce77dfdda9fd1cf5e9d796faf57d1cedef9803aec84a6d7f8487d32781341e")
	testPubKey2, _ = common.NewPubKeyFromStr("0314aaf9b2547682b81977b3ac0c5585c3521a0a5430fb410cb572d5c72364edf3")
)

func createTestLndClient(ctrl *gomock.Controller, pubKey common.PubKey) (
	*lnd.MockLndClient,
	chan *routerrpc.ForwardHtlcInterceptRequest,
	chan *routerrpc.ForwardHtlcInterceptResponse,
	chan *chainrpc.BlockEpoch) {

	lndClient := lnd.NewMockLndClient(ctrl)
	lndClient.EXPECT().PubKey().Return(pubKey).AnyTimes()

	htlcChan := make(chan *routerrpc.ForwardHtlcInterceptRequest)
	responseChan := make(chan *routerrpc.ForwardHtlcInterceptResponse, 1)
	lndClient.EXPECT().HtlcInterceptor(gomock.Any()).
		Return(
			func(response *routerrpc.ForwardHtlcInterceptResponse) error {
				responseChan <- response

				return nil
			},
			func() (*routerrpc.ForwardHtlcInterceptRequest, error) {
				return <-htlcChan, nil
			},
			nil,
		)

	blockChan := make(chan *chainrpc.BlockEpoch)
	lndClient.EXPECT().RegisterBlockEpochNtfn(gomock.Any()).
		Return(blockChan, nil, nil)

	return lndClient, htlcChan, responseChan, blockChan
}

func TestMux(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	var testKey = [32]byte{
		0x81, 0xb6, 0x37, 0xd8, 0xfc, 0xd2, 0xc6, 0xda,
		0x68, 0x59, 0xe6, 0x96, 0x31, 0x13, 0xa1, 0x17,
		0xd, 0xe7, 0x93, 0xe4, 0xb7, 0x25, 0xb8, 0x4d,
		0x1e, 0xb, 0x4c, 0xf9, 0x9e, 0xc5, 0x8c, 0xe9,
	}

	keyRing := NewKeyRing(testKey)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	lndClient1, htlcChan1, responseChan1, blockChan1 := createTestLndClient(ctrl, testPubKey1)
	lndClient2, htlcChan2, responseChan2, blockChan2 := createTestLndClient(ctrl, testPubKey2)

	activeNetParams := &chaincfg.RegressionNetParams

	db := &MockInvoiceDb{}

	creator, err := NewInvoiceCreator(
		&InvoiceCreatorConfig{
			KeyRing:         keyRing,
			GwPubKeys:       []common.PubKey{testPubKey1, testPubKey2},
			ActiveNetParams: activeNetParams,
		},
	)
	require.NoError(t, err)

	invoice, hash, err := creator.Create(10000, time.Minute, "test", 40)
	require.NoError(t, err)
	require.NotNil(t, invoice)

	// Store invoice.
	db.Invoice = &invoice.InvoiceCreationData
	db.Hash = hash

	mux, err := New(&MuxConfig{
		KeyRing:         keyRing,
		ActiveNetParams: activeNetParams,
		Lnd:             []lnd.LndClient{lndClient1, lndClient2},
		ChannelDb:       db,
		Logger:          logger.Sugar(),
	})
	require.NoError(t, err)

	require.NoError(t, mux.Start())

	// Send initial block heights.
	blockChan1 <- &chainrpc.BlockEpoch{Height: 1000}
	blockChan2 <- &chainrpc.BlockEpoch{Height: 1000}

	// Generate data for test htlc.
	var testHash = lntypes.Hash{1, 2, 3}

	dest, err := route.NewVertexFromBytes(keyRing.pubKey.SerializeCompressed())
	require.NoError(t, err)

	route := &route.Route{
		Hops: []*route.Hop{
			{
				PubKeyBytes: dest,
				MPP: record.NewMPP(
					10000, invoice.PaymentAddr,
				),
			},
		},
	}

	sessionKey, err := btcec.NewPrivateKey()
	require.NoError(t, err)

	onionBlob, err := generateSphinxPacket(route, testHash[:], sessionKey)
	require.NoError(t, err)

	receiveHtlc := func(htlcID uint64, amt int64) *routerrpc.ForwardHtlcInterceptRequest {
		return &routerrpc.ForwardHtlcInterceptRequest{
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
		expectedAction routerrpc.ResolveHoldForwardAction) {

		require.Equal(t, expectedAction, resp.Action)
	}

	// Test settle error.
	db.SettleErr = errors.New("settle error")
	htlcChan1 <- receiveHtlc(0, 10000)
	expectResponse(<-responseChan1, routerrpc.ResolveHoldForwardAction_FAIL)

	// Notify arrival of part 1.
	db.SettleErr = nil
	htlcChan1 <- receiveHtlc(0, 6000)

	// Replay arrival of part 1.
	htlcChan1 <- receiveHtlc(0, 6000)

	// Let it time out. Expect two responses, one for each notified arrival.
	expectResponse(<-responseChan1, routerrpc.ResolveHoldForwardAction_FAIL)
	expectResponse(<-responseChan1, routerrpc.ResolveHoldForwardAction_FAIL)

	// Notify arrival of part 1.
	htlcChan1 <- receiveHtlc(1, 6000)

	// Notify arrival of part 2.
	htlcChan2 <- receiveHtlc(2, 4000)

	expectResponse(<-responseChan1, routerrpc.ResolveHoldForwardAction_SETTLE)

	expectResponse(<-responseChan2, routerrpc.ResolveHoldForwardAction_SETTLE)

	require.NotEmpty(t, db.Htlcs)

	// Replay settled htlc.
	htlcChan1 <- receiveHtlc(1, 6000)
	expectResponse(<-responseChan1, routerrpc.ResolveHoldForwardAction_SETTLE)

	// New payment to settled invoice
	htlcChan1 <- receiveHtlc(10, 10000)
	expectResponse(<-responseChan1, routerrpc.ResolveHoldForwardAction_FAIL)

	require.NoError(t, mux.Stop())
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
