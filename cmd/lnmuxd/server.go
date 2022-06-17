package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/bottlepay/lnmux"
	"github.com/bottlepay/lnmux/cmd/lnmuxd/lnmux_proto"
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
	case persistence.InvoiceStateOpen:
		return lnmux_proto.SubscribeSingleInvoiceResponse_STATE_OPEN

	case persistence.InvoiceStateAccepted:
		return lnmux_proto.SubscribeSingleInvoiceResponse_STATE_ACCEPTED

	case persistence.InvoiceStateSettleRequested:
		return lnmux_proto.SubscribeSingleInvoiceResponse_STATE_SETTLE_REQUESTED

	case persistence.InvoiceStateSettled:
		return lnmux_proto.SubscribeSingleInvoiceResponse_STATE_SETTLED

	case persistence.InvoiceStateCancelled:
		return lnmux_proto.SubscribeSingleInvoiceResponse_STATE_CANCELLED

	default:
		panic("unknown invoice state")
	}
}

func marshallCancelledReason(reason persistence.CancelledReason) lnmux_proto.SubscribeSingleInvoiceResponse_CancelledReason {
	switch reason {
	case persistence.CancelledReasonNone:
		return lnmux_proto.SubscribeSingleInvoiceResponse_REASON_NONE

	case persistence.CancelledReasonExpired:
		return lnmux_proto.SubscribeSingleInvoiceResponse_REASON_EXPIRED

	case persistence.CancelledReasonAcceptTimeout:
		return lnmux_proto.SubscribeSingleInvoiceResponse_REASON_ACCEPT_TIMEOUT

	case persistence.CancelledReasonExternal:
		return lnmux_proto.SubscribeSingleInvoiceResponse_REASON_EXTERNAL

	default:
		panic("unknown cancelled reason")
	}
}

func (s *server) SubscribeSingleInvoice(req *lnmux_proto.SubscribeSingleInvoiceRequest,
	subscription lnmux_proto.Service_SubscribeSingleInvoiceServer) error {

	hash, err := lntypes.MakeHash(req.Hash)
	if err != nil {
		return err
	}

	var (
		updateChan = make(chan lnmux.InvoiceUpdate, subscriberQueueSize)
		closed     = false
	)

	cancel, err := s.registry.Subscribe(
		hash,
		func(update lnmux.InvoiceUpdate) {
			// Don't try to write if the channel is closed. This callback does
			// not need to be thread-safe.
			if closed {
				return
			}

			select {
			case updateChan <- update:

			// When the context is cancelled, close the update channel. We don't
			// want to skip this update and on the next one send into the
			// channel again.
			case <-subscription.Context().Done():
				close(updateChan)
				closed = true

			// When the update channel is full, terminate the subscriber to
			// prevent blocking multiplexer.
			default:
				close(updateChan)
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

		case update, ok := <-updateChan:
			// If the channel gets closed, disconnect the client.
			if !ok {
				return errors.New("buffer overflow")
			}

			err := subscription.Send(&lnmux_proto.SubscribeSingleInvoiceResponse{
				State:           marshallInvoiceState(update.State),
				CancelledReason: marshallCancelledReason(update.CancelledReason),
			})
			if err != nil {
				return err
			}
		}
	}
}

func (s *server) AddInvoice(ctx context.Context,
	req *lnmux_proto.AddInvoiceRequest) (*lnmux_proto.AddInvoiceResponse,
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
	expiryTime := time.Now().Add(expiry)
	invoice, preimage, err := s.creator.Create(
		req.AmtMsat, expiry, req.Description, descHash, finalCltvExpiry,
	)
	if err != nil {
		return nil, err
	}

	hash := preimage.Hash()

	// Store invoice.
	creationData := &persistence.InvoiceCreationData{
		InvoiceCreationData: invoice.InvoiceCreationData,
		CreatedAt:           invoice.CreationDate,
		ID:                  int64(rand.Int31()),
		PaymentRequest:      invoice.PaymentRequest,
		ExpiresAt:           expiryTime,
		AutoSettle:          req.AutoSettle,
	}

	err = s.registry.NewInvoice(creationData)
	if err != nil {
		return nil, err
	}

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
	if err != nil {
		return nil, err
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
	if err != nil {
		return nil, err
	}

	return &lnmux_proto.CancelInvoiceResponse{}, nil
}
