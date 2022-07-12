package main

import (
	"context"
	"time"

	"github.com/bottlepay/lnmux"
	"github.com/bottlepay/lnmux/cmd/lnmuxd/lnmux_proto"
	"github.com/bottlepay/lnmux/persistence"
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

	lnmux_proto.UnimplementedServiceServer
}

func newServer(creator *lnmux.InvoiceCreator, registry *lnmux.InvoiceRegistry) (*server, error) {
	return &server{
		registry: registry,
		creator:  creator,
	}, nil
}

func marshallInvoiceState(state persistence.InvoiceState) lnmux_proto.SubscribeSingleInvoiceResponse_InvoiceState {
	switch state {

	case persistence.InvoiceStateAccepted:
		return lnmux_proto.SubscribeSingleInvoiceResponse_STATE_ACCEPTED

	case persistence.InvoiceStateSettleRequested:
		return lnmux_proto.SubscribeSingleInvoiceResponse_STATE_SETTLE_REQUESTED

	case persistence.InvoiceStateSettled:
		return lnmux_proto.SubscribeSingleInvoiceResponse_STATE_SETTLED

	default:
		panic("unknown invoice state")
	}
}

func (s *server) SubscribeSingleInvoice(req *lnmux_proto.SubscribeSingleInvoiceRequest,
	subscription lnmux_proto.Service_SubscribeSingleInvoiceServer) error {

	hash, err := lntypes.MakeHash(req.Hash)
	if err != nil {
		return err
	}

	return streamBuffered(
		subscription.Context(),
		func(cb func(lnmux.InvoiceUpdate)) (func(), error) {
			return s.registry.Subscribe(hash, cb)
		},
		func(update lnmux.InvoiceUpdate) error {
			return subscription.Send(&lnmux_proto.SubscribeSingleInvoiceResponse{
				State: marshallInvoiceState(update.State),
			})
		},
	)
}

func streamBuffered[T any](ctx context.Context,
	subscribe func(func(T)) (func(), error),
	send func(T) error) error {

	var (
		bufferChan = make(chan T, subscriberQueueSize)
		closed     = false
	)

	cancel, err := subscribe(
		func(item T) {
			// Don't try to write if the channel is closed. This callback does
			// not need to be thread-safe.
			if closed {
				return
			}

			select {
			case bufferChan <- item:

			// When the context is cancelled, close the update channel. We don't
			// want to skip this update and on the next one send into the
			// channel again.
			case <-ctx.Done():
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

		case <-ctx.Done():
			return nil

		case item, ok := <-bufferChan:
			// If the channel gets closed, disconnect the client.
			if !ok {
				return status.Error(
					codes.ResourceExhausted, "buffer overflow",
				)
			}

			err := send(item)
			if err != nil {
				return err
			}
		}
	}
}

func (s *server) SubscribePaymentAccepted(req *lnmux_proto.SubscribePaymentAcceptedRequest,
	subscription lnmux_proto.Service_SubscribePaymentAcceptedServer) error {

	return streamBuffered(
		subscription.Context(),
		func(cb func(lntypes.Hash)) (func(), error) {
			return s.registry.SubscribeAccept(cb)
		},
		func(hash lntypes.Hash) error {
			return subscription.Send(&lnmux_proto.SubscribePaymentAcceptedResponse{
				Hash: hash[:],
			})
		},
	)
}

func (s *server) AddInvoice(ctx context.Context,
	req *lnmux_proto.AddInvoiceRequest) (*lnmux_proto.AddInvoiceResponse,
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

	return &lnmux_proto.AddInvoiceResponse{
		PaymentRequest: invoice.PaymentRequest,
		Hash:           hash[:],
		Preimage:       preimage[:],
	}, nil
}

func (s *server) SettleInvoice(ctx context.Context,
	req *lnmux_proto.SettleInvoiceRequest) (*lnmux_proto.SettleInvoiceResponse,
	error) {

	hash, err := lntypes.MakeHash(req.Hash)
	if err != nil {
		return nil, err
	}

	err = s.registry.RequestSettle(hash)
	switch {
	case err == lnmux.ErrInvoiceNotFound:
		return nil, status.Error(
			codes.NotFound, lnmux.ErrInvoiceNotFound.Error(),
		)

	case err == lnmux.ErrAutoSettling:
		return nil, status.Error(
			codes.Unavailable, lnmux.ErrAutoSettling.Error(),
		)

	case err != nil:
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	return &lnmux_proto.SettleInvoiceResponse{}, nil
}

func (s *server) CancelInvoice(ctx context.Context,
	req *lnmux_proto.CancelInvoiceRequest) (*lnmux_proto.CancelInvoiceResponse,
	error) {

	hash, err := lntypes.MakeHash(req.Hash)
	if err != nil {
		return nil, err
	}

	err = s.registry.CancelInvoice(hash)
	switch {
	case err == lnmux.ErrInvoiceNotFound:
		return nil, status.Error(codes.NotFound, lnmux.ErrInvoiceNotFound.Error())

	case err != nil:
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	return &lnmux_proto.CancelInvoiceResponse{}, nil
}
