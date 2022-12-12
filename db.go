package lnmux

import (
	"github.com/bottlepay/lnmux/types"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
)

func newCircuitKeyFromRPC(key *routerrpc.CircuitKey) types.CircuitKey {
	return types.CircuitKey{
		ChanID: key.ChanId,
		HtlcID: key.HtlcId,
	}
}
