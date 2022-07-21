package main

import (
	"context"
	"errors"
	"time"

	"github.com/bottlepay/lnmux"
	"github.com/bottlepay/lnmux/lnmuxrpc"
	"github.com/bottlepay/lnmux/types"
	"github.com/lightningnetwork/lnd/lntypes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// finalCltvExpiry is the minimally required number of blocks before htlc
	// expiry for an htlc to be accepted.
	finalCltvExpiry = 40

	// subscriberQueueSize is the length of the rpc subscriber queue. If the
	// queue becomes full, the client is disconnected.
	subscriberQueueSize = 100
)

type server struct {
	registry *lnmux.InvoiceRegistry
	creator  *lnmux.InvoiceCreator

	lnmuxrpc.UnimplementedServiceServer
}

func newServer(creator *lnmux.InvoiceCreator,
	registry *lnmux.InvoiceRegistry) *server {

	return &server{
		registry: registry,
		creator:  creator,
	}
}

func (s *server) GetInfo(ctx context.Context,
	req *lnmuxrpc.GetInfoRequest) (*lnmuxrpc.GetInfoResponse,
	error) {

	var nodes []*lnmuxrpc.NodeInfo
	for _, key := range s.creator.NodePubKeys() {
		nodes = append(nodes, &lnmuxrpc.NodeInfo{
			PubKey: key.SerializeCompressed(),
		})
	}

	network := s.creator.Network().Name

	// Convert testnet3 to common name testnet.
	if network == "testnet3" {
		network = "testnet"
	}

	return &lnmuxrpc.GetInfoResponse{
		AutoSettle: s.registry.AutoSettle(),
		Nodes:      nodes,
		PubKey:     s.creator.PubKey().SerializeCompressed(),
		Network:    network,
	}, nil
}

type acceptedEvent struct {
	hash  lntypes.Hash
	setID [32]byte
}

func (s *server) SubscribeInvoiceAccepted(req *lnmuxrpc.SubscribeInvoiceAcceptedRequest,
	subscription lnmuxrpc.Service_SubscribeInvoiceAcceptedServer) error {

	var (
		bufferChan = make(chan acceptedEvent, subscriberQueueSize)
		closed     = false
	)

	cancel, err := s.registry.SubscribeAccept(
		func(hash lntypes.Hash, setID lnmux.SetID) {
			// Don't try to write if the channel is closed. This callback does
			// not need to be thread-safe.
			if closed {
				return
			}

			select {
			case bufferChan <- acceptedEvent{
				hash:  hash,
				setID: setID,
			}:

			// When the context is cancelled, close the update channel. We don't
			// want to skip this update and on the next one send into the
			// channel again.
			case <-subscription.Context().Done():
				close(bufferChan)
				closed = true

			// When the update channel is full, terminate the subscriber to
			// prevent blocking multiplexer.
			default:
				close(bufferChan)
				closed = true
			}
		},
	)
	if err != nil {
		return err
	}
	defer cancel()

	for {
		select {

		case <-subscription.Context().Done():
			return nil

		case item, ok := <-bufferChan:
			// If the channel gets closed, disconnect the client.
			if !ok {
				return status.Error(
					codes.ResourceExhausted, "buffer overflow",
				)
			}

			err := subscription.Send(&lnmuxrpc.SubscribeInvoiceAcceptedResponse{
				Hash:  item.hash[:],
				SetId: item.setID[:],
			})
			if err != nil {
				return err
			}
		}
	}
}

func (s *server) AddInvoice(ctx context.Context,
	req *lnmuxrpc.AddInvoiceRequest) (*lnmuxrpc.AddInvoiceResponse,
	error) {

	// Validate inputs.
	if req.ExpirySecs <= 0 {
		return nil, status.Error(
			codes.InvalidArgument, "expiry_secs not specified",
		)
	}

	if req.AmtMsat <= 0 {
		return nil, status.Error(
			codes.InvalidArgument, "amt_msat not specified",
		)
	}

	var descHash *lntypes.Hash
	if len(req.DescriptionHash) > 0 {
		hash, err := lntypes.MakeHash(req.DescriptionHash)
		if err != nil {
			return nil, status.Errorf(
				codes.InvalidArgument, "invalid desc hash: %v", err,
			)
		}
		descHash = &hash
	}

	// Create the invoice.
	expiry := time.Duration(req.ExpirySecs) * time.Second
	invoice, preimage, err := s.creator.Create(
		req.AmtMsat, expiry, req.Description, descHash, finalCltvExpiry,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	hash := preimage.Hash()

	return &lnmuxrpc.AddInvoiceResponse{
		PaymentRequest: invoice.PaymentRequest,
		Hash:           hash[:],
		Preimage:       preimage[:],
	}, nil
}

func parseSetID(setIDBytes []byte) ([32]byte, error) {
	if len(setIDBytes) != 32 {
		return [32]byte{}, errors.New("invalid set id")
	}

	var setID [32]byte
	copy(setID[:], setIDBytes)

	return setID, nil
}

func (s *server) SettleInvoice(ctx context.Context,
	req *lnmuxrpc.SettleInvoiceRequest) (*lnmuxrpc.SettleInvoiceResponse,
	error) {

	hash, err := lntypes.MakeHash(req.Hash)
	if err != nil {
		return nil, err
	}

	setID, err := parseSetID(req.SetId)
	if err != nil {
		return nil, err
	}

	err = s.registry.RequestSettle(hash, setID)
	switch {
	case err == types.ErrInvoiceNotFound:
		return nil, status.Error(
			codes.NotFound, types.ErrInvoiceNotFound.Error(),
		)

	case err == lnmux.ErrAutoSettling:
		return nil, status.Error(
			codes.Unavailable, lnmux.ErrAutoSettling.Error(),
		)

	case err != nil:
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	return &lnmuxrpc.SettleInvoiceResponse{}, nil
}

func (s *server) CancelInvoice(ctx context.Context,
	req *lnmuxrpc.CancelInvoiceRequest) (*lnmuxrpc.CancelInvoiceResponse,
	error) {

	hash, err := lntypes.MakeHash(req.Hash)
	if err != nil {
		return nil, err
	}

	setID, err := parseSetID(req.SetId)
	if err != nil {
		return nil, err
	}

	err = s.registry.CancelInvoice(hash, setID)
	switch {
	case err == types.ErrInvoiceNotFound:
		return nil, status.Error(
			codes.NotFound, types.ErrInvoiceNotFound.Error(),
		)

	case err == lnmux.ErrSettleRequested:
		return nil, status.Error(codes.FailedPrecondition, err.Error())

	case err != nil:
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	return &lnmuxrpc.CancelInvoiceResponse{}, nil
}
