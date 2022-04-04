package lnmux

import (
	"crypto/rand"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/lightningnetwork/lnd/feature"
	"github.com/lightningnetwork/lnd/keychain"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/netann"
	"github.com/lightningnetwork/lnd/zpay32"

	"github.com/bottlepay/lnmux/common"
)

const virtualChannel = 12345

type InvoiceCreatorConfig struct {
	KeyRing         keychain.SecretKeyRing
	GwPubKeys       []common.PubKey
	ActiveNetParams *chaincfg.Params
}

type InvoiceCreator struct {
	keyRing         keychain.SecretKeyRing
	gwPubKeys       []*btcec.PublicKey
	activeNetParams *chaincfg.Params
	idKeyDesc       keychain.KeyDescriptor
}

func NewInvoiceCreator(cfg *InvoiceCreatorConfig) (*InvoiceCreator, error) {
	idKeyDesc, err := cfg.KeyRing.DeriveKey(
		keychain.KeyLocator{
			Family: keychain.KeyFamilyNodeKey,
			Index:  0,
		},
	)
	if err != nil {
		return nil, err
	}

	var parsedPubKeys []*btcec.PublicKey
	for _, pubKey := range cfg.GwPubKeys {
		parsedPubKey, err := btcec.ParsePubKey(pubKey[:])
		if err != nil {
			return nil, err
		}
		parsedPubKeys = append(parsedPubKeys, parsedPubKey)
	}

	return &InvoiceCreator{
		gwPubKeys:       parsedPubKeys,
		activeNetParams: cfg.ActiveNetParams,
		keyRing:         cfg.KeyRing,
		idKeyDesc:       idKeyDesc,
	}, nil
}

// TODO: Add description hash.
func (c *InvoiceCreator) Create(amtMSat int64, expiry time.Duration,
	memo string, cltvDelta uint64) (*Invoice, lntypes.Hash,
	error) {

	// Get features.
	featureMgr, err := feature.NewManager(feature.Config{})
	if err != nil {
		return nil, lntypes.Hash{}, err
	}

	nodeKeySigner := keychain.NewPubKeyMessageSigner(
		c.idKeyDesc.PubKey, c.idKeyDesc.KeyLocator, c.keyRing,
	)

	nodeSigner := netann.NewNodeSigner(nodeKeySigner)

	paymentPreimage := &lntypes.Preimage{}
	if _, err := rand.Read(paymentPreimage[:]); err != nil {
		return nil, lntypes.Hash{}, err
	}
	paymentHash := paymentPreimage.Hash()

	// We also create an encoded payment request which allows the
	// caller to compactly send the invoice to the payer. We'll create a
	// list of options to be added to the encoded payment request. For now
	// we only support the required fields description/description_hash,
	// expiry, fallback address, and the amount field.
	var options []func(*zpay32.Invoice)

	// We only include the amount in the invoice if it is greater than 0.
	// By not including the amount, we enable the creation of invoices that
	// allow the payee to specify the amount of satoshis they wish to send.
	if amtMSat > 0 {
		options = append(options,
			zpay32.Amount(lnwire.MilliSatoshi(amtMSat)),
		)
	}

	options = append(options, zpay32.Expiry(expiry))

	// Use the memo field as the description. If this is not set
	// this will just be an empty string.
	options = append(options, zpay32.Description(memo))

	options = append(options, zpay32.CLTVExpiry(cltvDelta))

	// Add virtual hop hints.
	for _, gwPubKey := range c.gwPubKeys {
		hopHint := zpay32.HopHint{
			NodeID:                    gwPubKey,
			ChannelID:                 virtualChannel, // Rotate?
			FeeBaseMSat:               0,
			FeeProportionalMillionths: 0,
			CLTVExpiryDelta:           40, // Can be zero?
		}

		options = append(options, zpay32.RouteHint([]zpay32.HopHint{hopHint}))
	}

	// Set our desired invoice features and add them to our list of options.
	invoiceFeatures := featureMgr.Get(feature.SetInvoice)
	options = append(options, zpay32.Features(invoiceFeatures))

	// Generate and set a random payment address for this invoice. If the
	// sender understands payment addresses, this can be used to avoid
	// intermediaries probing the receiver.
	var paymentAddr [32]byte
	if _, err := rand.Read(paymentAddr[:]); err != nil {
		return nil, lntypes.Hash{}, err
	}
	options = append(options, zpay32.PaymentAddr(paymentAddr))

	// Create and encode the payment request as a bech32 (zpay32) string.
	creationDate := time.Now()
	payReq, err := zpay32.NewInvoice(
		c.activeNetParams, paymentHash, creationDate, options...,
	)
	if err != nil {
		return nil, lntypes.Hash{}, err
	}

	payReqString, err := payReq.Encode(zpay32.MessageSigner{
		SignCompact: func(msg []byte) ([]byte, error) {
			return nodeSigner.SignMessageCompact(msg, false)
		},
	})
	if err != nil {
		return nil, lntypes.Hash{}, err
	}

	newInvoice := &Invoice{
		CreationDate:   creationDate,
		PaymentRequest: payReqString,
		InvoiceCreationData: InvoiceCreationData{
			FinalCltvDelta:  int32(payReq.MinFinalCLTVExpiry()),
			Value:           lnwire.MilliSatoshi(amtMSat),
			PaymentPreimage: *paymentPreimage,
			PaymentAddr:     paymentAddr,
		},
	}

	return newInvoice, paymentHash, nil
}
