package lnd

import (
	"context"
	"os"
	"time"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_opentracing "github.com/grpc-ecosystem/go-grpc-middleware/tracing/opentracing"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/lightningnetwork/lnd/macaroons"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"gopkg.in/macaroon.v2"
)

// Define global lnd client timeouts.
const (
	unaryTimeout  = time.Minute
	streamTimeout = 30 * time.Minute
)

// loadGrpcConn loads configurations and attempts a grpc connection to an LND node.
func loadGrpcConn(tlsPath, macaroonPath, url string) (*grpc.ClientConn, error) {
	tlsCreds, err := credentials.NewClientTLSFromFile(tlsPath, "")
	if err != nil {
		return nil, err
	}

	macaroonBytes, err := os.ReadFile(macaroonPath)
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
				timeoutUnaryInterceptor(unaryTimeout),
			),
		),
		grpc.WithStreamInterceptor(
			grpc_middleware.ChainStreamClient(
				grpc_opentracing.StreamClientInterceptor(),
				grpc_prometheus.StreamClientInterceptor,
				timeoutStreamInterceptor(streamTimeout),
			),
		),
	}

	return grpc.Dial(url, opts...)
}

func timeoutUnaryInterceptor(timeout time.Duration) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {

		timedCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		return invoker(timedCtx, method, req, reply, cc, opts...)
	}
}

func timeoutStreamInterceptor(timeout time.Duration) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string,
		streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {

		timedCtx, _ := context.WithTimeout(ctx, timeout) // nolint:govet

		return streamer(timedCtx, desc, cc, method, opts...)
	}
}
