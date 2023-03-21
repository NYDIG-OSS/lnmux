// Code generated by protoc-gen-go-grpc. DO NOT EDIT.

package lnmuxrpc

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

// ServiceClient is the client API for Service service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type ServiceClient interface {
	GetInfo(ctx context.Context, in *GetInfoRequest, opts ...grpc.CallOption) (*GetInfoResponse, error)
	AddInvoice(ctx context.Context, in *AddInvoiceRequest, opts ...grpc.CallOption) (*AddInvoiceResponse, error)
	SubscribeInvoiceAccepted(ctx context.Context, in *SubscribeInvoiceAcceptedRequest, opts ...grpc.CallOption) (Service_SubscribeInvoiceAcceptedClient, error)
	WaitForInvoiceFinalStatus(ctx context.Context, in *WaitForInvoiceFinalStatusRequest, opts ...grpc.CallOption) (*WaitForInvoiceFinalStatusResponse, error)
	// Requests settlement for an accepted invoice. This call is idempotent.
	SettleInvoice(ctx context.Context, in *SettleInvoiceRequest, opts ...grpc.CallOption) (*SettleInvoiceResponse, error)
	// Cancels an accepted invoice. In case settle has been requested
	// for an invoice, CancelInvoice returns a FailedPrecondition error.
	CancelInvoice(ctx context.Context, in *CancelInvoiceRequest, opts ...grpc.CallOption) (*CancelInvoiceResponse, error)
	// Lists settled or to be settled invoices. It returns invoices with a sequence number between
	// 'sequence_start' and 'sequence_start' + max_invoices_count'.
	ListInvoices(ctx context.Context, in *ListInvoicesRequest, opts ...grpc.CallOption) (*ListInvoicesResponse, error)
}

type serviceClient struct {
	cc grpc.ClientConnInterface
}

func NewServiceClient(cc grpc.ClientConnInterface) ServiceClient {
	return &serviceClient{cc}
}

func (c *serviceClient) GetInfo(ctx context.Context, in *GetInfoRequest, opts ...grpc.CallOption) (*GetInfoResponse, error) {
	out := new(GetInfoResponse)
	err := c.cc.Invoke(ctx, "/lnmux.Service/GetInfo", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *serviceClient) AddInvoice(ctx context.Context, in *AddInvoiceRequest, opts ...grpc.CallOption) (*AddInvoiceResponse, error) {
	out := new(AddInvoiceResponse)
	err := c.cc.Invoke(ctx, "/lnmux.Service/AddInvoice", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *serviceClient) SubscribeInvoiceAccepted(ctx context.Context, in *SubscribeInvoiceAcceptedRequest, opts ...grpc.CallOption) (Service_SubscribeInvoiceAcceptedClient, error) {
	stream, err := c.cc.NewStream(ctx, &Service_ServiceDesc.Streams[0], "/lnmux.Service/SubscribeInvoiceAccepted", opts...)
	if err != nil {
		return nil, err
	}
	x := &serviceSubscribeInvoiceAcceptedClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type Service_SubscribeInvoiceAcceptedClient interface {
	Recv() (*SubscribeInvoiceAcceptedResponse, error)
	grpc.ClientStream
}

type serviceSubscribeInvoiceAcceptedClient struct {
	grpc.ClientStream
}

func (x *serviceSubscribeInvoiceAcceptedClient) Recv() (*SubscribeInvoiceAcceptedResponse, error) {
	m := new(SubscribeInvoiceAcceptedResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *serviceClient) WaitForInvoiceFinalStatus(ctx context.Context, in *WaitForInvoiceFinalStatusRequest, opts ...grpc.CallOption) (*WaitForInvoiceFinalStatusResponse, error) {
	out := new(WaitForInvoiceFinalStatusResponse)
	err := c.cc.Invoke(ctx, "/lnmux.Service/WaitForInvoiceFinalStatus", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *serviceClient) SettleInvoice(ctx context.Context, in *SettleInvoiceRequest, opts ...grpc.CallOption) (*SettleInvoiceResponse, error) {
	out := new(SettleInvoiceResponse)
	err := c.cc.Invoke(ctx, "/lnmux.Service/SettleInvoice", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *serviceClient) CancelInvoice(ctx context.Context, in *CancelInvoiceRequest, opts ...grpc.CallOption) (*CancelInvoiceResponse, error) {
	out := new(CancelInvoiceResponse)
	err := c.cc.Invoke(ctx, "/lnmux.Service/CancelInvoice", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *serviceClient) ListInvoices(ctx context.Context, in *ListInvoicesRequest, opts ...grpc.CallOption) (*ListInvoicesResponse, error) {
	out := new(ListInvoicesResponse)
	err := c.cc.Invoke(ctx, "/lnmux.Service/ListInvoices", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ServiceServer is the server API for Service service.
// All implementations must embed UnimplementedServiceServer
// for forward compatibility
type ServiceServer interface {
	GetInfo(context.Context, *GetInfoRequest) (*GetInfoResponse, error)
	AddInvoice(context.Context, *AddInvoiceRequest) (*AddInvoiceResponse, error)
	SubscribeInvoiceAccepted(*SubscribeInvoiceAcceptedRequest, Service_SubscribeInvoiceAcceptedServer) error
	WaitForInvoiceFinalStatus(context.Context, *WaitForInvoiceFinalStatusRequest) (*WaitForInvoiceFinalStatusResponse, error)
	// Requests settlement for an accepted invoice. This call is idempotent.
	SettleInvoice(context.Context, *SettleInvoiceRequest) (*SettleInvoiceResponse, error)
	// Cancels an accepted invoice. In case settle has been requested
	// for an invoice, CancelInvoice returns a FailedPrecondition error.
	CancelInvoice(context.Context, *CancelInvoiceRequest) (*CancelInvoiceResponse, error)
	// Lists settled or to be settled invoices. It returns invoices with a sequence number between
	// 'sequence_start' and 'sequence_start' + max_invoices_count'.
	ListInvoices(context.Context, *ListInvoicesRequest) (*ListInvoicesResponse, error)
	mustEmbedUnimplementedServiceServer()
}

// UnimplementedServiceServer must be embedded to have forward compatible implementations.
type UnimplementedServiceServer struct {
}

func (UnimplementedServiceServer) GetInfo(context.Context, *GetInfoRequest) (*GetInfoResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetInfo not implemented")
}
func (UnimplementedServiceServer) AddInvoice(context.Context, *AddInvoiceRequest) (*AddInvoiceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method AddInvoice not implemented")
}
func (UnimplementedServiceServer) SubscribeInvoiceAccepted(*SubscribeInvoiceAcceptedRequest, Service_SubscribeInvoiceAcceptedServer) error {
	return status.Errorf(codes.Unimplemented, "method SubscribeInvoiceAccepted not implemented")
}
func (UnimplementedServiceServer) WaitForInvoiceFinalStatus(context.Context, *WaitForInvoiceFinalStatusRequest) (*WaitForInvoiceFinalStatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method WaitForInvoiceFinalStatus not implemented")
}
func (UnimplementedServiceServer) SettleInvoice(context.Context, *SettleInvoiceRequest) (*SettleInvoiceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SettleInvoice not implemented")
}
func (UnimplementedServiceServer) CancelInvoice(context.Context, *CancelInvoiceRequest) (*CancelInvoiceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CancelInvoice not implemented")
}
func (UnimplementedServiceServer) ListInvoices(context.Context, *ListInvoicesRequest) (*ListInvoicesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListInvoices not implemented")
}
func (UnimplementedServiceServer) mustEmbedUnimplementedServiceServer() {}

// UnsafeServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to ServiceServer will
// result in compilation errors.
type UnsafeServiceServer interface {
	mustEmbedUnimplementedServiceServer()
}

func RegisterServiceServer(s grpc.ServiceRegistrar, srv ServiceServer) {
	s.RegisterService(&Service_ServiceDesc, srv)
}

func _Service_GetInfo_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetInfoRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ServiceServer).GetInfo(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/lnmux.Service/GetInfo",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ServiceServer).GetInfo(ctx, req.(*GetInfoRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Service_AddInvoice_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(AddInvoiceRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ServiceServer).AddInvoice(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/lnmux.Service/AddInvoice",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ServiceServer).AddInvoice(ctx, req.(*AddInvoiceRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Service_SubscribeInvoiceAccepted_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(SubscribeInvoiceAcceptedRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(ServiceServer).SubscribeInvoiceAccepted(m, &serviceSubscribeInvoiceAcceptedServer{stream})
}

type Service_SubscribeInvoiceAcceptedServer interface {
	Send(*SubscribeInvoiceAcceptedResponse) error
	grpc.ServerStream
}

type serviceSubscribeInvoiceAcceptedServer struct {
	grpc.ServerStream
}

func (x *serviceSubscribeInvoiceAcceptedServer) Send(m *SubscribeInvoiceAcceptedResponse) error {
	return x.ServerStream.SendMsg(m)
}

func _Service_WaitForInvoiceFinalStatus_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(WaitForInvoiceFinalStatusRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ServiceServer).WaitForInvoiceFinalStatus(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/lnmux.Service/WaitForInvoiceFinalStatus",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ServiceServer).WaitForInvoiceFinalStatus(ctx, req.(*WaitForInvoiceFinalStatusRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Service_SettleInvoice_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SettleInvoiceRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ServiceServer).SettleInvoice(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/lnmux.Service/SettleInvoice",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ServiceServer).SettleInvoice(ctx, req.(*SettleInvoiceRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Service_CancelInvoice_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CancelInvoiceRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ServiceServer).CancelInvoice(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/lnmux.Service/CancelInvoice",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ServiceServer).CancelInvoice(ctx, req.(*CancelInvoiceRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Service_ListInvoices_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListInvoicesRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ServiceServer).ListInvoices(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/lnmux.Service/ListInvoices",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ServiceServer).ListInvoices(ctx, req.(*ListInvoicesRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// Service_ServiceDesc is the grpc.ServiceDesc for Service service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var Service_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "lnmux.Service",
	HandlerType: (*ServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "GetInfo",
			Handler:    _Service_GetInfo_Handler,
		},
		{
			MethodName: "AddInvoice",
			Handler:    _Service_AddInvoice_Handler,
		},
		{
			MethodName: "WaitForInvoiceFinalStatus",
			Handler:    _Service_WaitForInvoiceFinalStatus_Handler,
		},
		{
			MethodName: "SettleInvoice",
			Handler:    _Service_SettleInvoice_Handler,
		},
		{
			MethodName: "CancelInvoice",
			Handler:    _Service_CancelInvoice_Handler,
		},
		{
			MethodName: "ListInvoices",
			Handler:    _Service_ListInvoices_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "SubscribeInvoiceAccepted",
			Handler:       _Service_SubscribeInvoiceAccepted_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "lnmux.proto",
}
