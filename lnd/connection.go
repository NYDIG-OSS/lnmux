package lnd

import (
	"io/ioutil"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_opentracing "github.com/grpc-ecosystem/go-grpc-middleware/tracing/opentracing"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/lightningnetwork/lnd/macaroons"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"gopkg.in/macaroon.v2"
)

// loadGrpcConn loads configurations and attempts a grpc connection to an LND node.
func loadGrpcConn(tlsPath, macaroonPath, url string) (*grpc.ClientConn, error) {
	tlsCreds, err := credentials.NewClientTLSFromFile(tlsPath, "")
	if err != nil {
		return nil, err
	}

	macaroonBytes, err := ioutil.ReadFile(macaroonPath)
	if err != nil {
		return nil, err
	}

	mac := &macaroon.Macaroon{}
	if err = mac.UnmarshalBinary(macaroonBytes); err != nil {
		return nil, err
	}

	macCreds, err := macaroons.NewMacaroonCredential(mac)
	if err != nil {
		return nil, err
	}

	// Make the grpc connection
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(tlsCreds),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1 * 1024 * 1024 * 50)), // 50Mb max message
		grpc.WithPerRPCCredentials(macCreds),
		grpc.WithUnaryInterceptor(
			grpc_middleware.ChainUnaryClient(
				grpc_opentracing.UnaryClientInterceptor(),
				grpc_prometheus.UnaryClientInterceptor,
			),
		),
		grpc.WithStreamInterceptor(
			grpc_middleware.ChainStreamClient(
				grpc_opentracing.StreamClientInterceptor(),
				grpc_prometheus.StreamClientInterceptor,
			),
		),
	}

	return grpc.Dial(url, opts...)
}
