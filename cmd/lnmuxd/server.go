package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bottlepay/lnmux"
	"github.com/bottlepay/lnmux/lnmuxrpc"
	"github.com/bottlepay/lnmux/persistence"
	"github.com/lightningnetwork/lnd/lntypes"
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

func newServer(creator *lnmux.InvoiceCreator, registry *lnmux.InvoiceRegistry) (*server, error) {
	return &server{
		registry: registry,
		creator:  creator,
	}, nil
}

func marshallInvoiceState(state persistence.InvoiceState) lnmuxrpc.SubscribeSingleInvoiceResponse_InvoiceState {
	switch state {

	case persistence.InvoiceStateAccepted:
		return lnmuxrpc.SubscribeSingleInvoiceResponse_STATE_ACCEPTED

	case persistence.InvoiceStateSettleRequested:
		return lnmuxrpc.SubscribeSingleInvoiceResponse_STATE_SETTLE_REQUESTED

	case persistence.InvoiceStateSettled:
		return lnmuxrpc.SubscribeSingleInvoiceResponse_STATE_SETTLED

	default:
		panic("unknown invoice state")
	}
}

func (s *server) SubscribeSingleInvoice(req *lnmuxrpc.SubscribeSingleInvoiceRequest,
	subscription lnmuxrpc.Service_SubscribeSingleInvoiceServer) error {

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
			return subscription.Send(&lnmuxrpc.SubscribeSingleInvoiceResponse{
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
				return errors.New("buffer overflow")
			}

			err := send(item)
			if err != nil {
				return err
			}
		}
	}
}

func (s *server) SubscribePaymentAccepted(req *lnmuxrpc.SubscribePaymentAcceptedRequest,
	subscription lnmuxrpc.Service_SubscribePaymentAcceptedServer) error {

	return streamBuffered(
		subscription.Context(),
		func(cb func(lntypes.Hash)) (func(), error) {
			return s.registry.SubscribeAccept(cb)
		},
		func(hash lntypes.Hash) error {
			return subscription.Send(&lnmuxrpc.SubscribePaymentAcceptedResponse{
				Hash: hash[:],
			})
		},
	)
}

func (s *server) AddInvoice(ctx context.Context,
	req *lnmuxrpc.AddInvoiceRequest) (*lnmuxrpc.AddInvoiceResponse,
	error) {

	// Validate inputs.
	if req.ExpirySecs <= 0 {
		return nil, errors.New("expiry_secs not specified")
	}

	if req.AmtMsat <= 0 {
		return nil, errors.New("amt_msat not specified")
	}

	var descHash *lntypes.Hash
	if len(req.DescriptionHash) > 0 {
		hash, err := lntypes.MakeHash(req.DescriptionHash)
		if err != nil {
			return nil, fmt.Errorf("invalid desc hash: %w", err)
		}
		descHash = &hash
	}

	// Create the invoice.
	expiry := time.Duration(req.ExpirySecs) * time.Second
	invoice, preimage, err := s.creator.Create(
		req.AmtMsat, expiry, req.Description, descHash, finalCltvExpiry,
	)
	if err != nil {
		return nil, err
	}

	hash := preimage.Hash()

	return &lnmuxrpc.AddInvoiceResponse{
		PaymentRequest: invoice.PaymentRequest,
		Hash:           hash[:],
		Preimage:       preimage[:],
	}, nil
}

func (s *server) SettleInvoice(ctx context.Context,
	req *lnmuxrpc.SettleInvoiceRequest) (*lnmuxrpc.SettleInvoiceResponse,
	error) {

	hash, err := lntypes.MakeHash(req.Hash)
	if err != nil {
		return nil, err
	}

	err = s.registry.RequestSettle(hash)
	if err != nil {
		return nil, err
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

	err = s.registry.CancelInvoice(hash)
	if err != nil {
		return nil, err
	}

	return &lnmuxrpc.CancelInvoiceResponse{}, nil
}
