// Hand-written equivalent of protoc-gen-go-grpc output for inference.proto.
//
// The Fedora repositories do not package protoc-gen-go-grpc, and this project installs
// build tools only from Fedora. The service is one unary method, so the stub is short.
// CI can regenerate it with `buf generate` if you prefer generated code.

package inferencev1

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const Gateway_Classify_FullMethodName = "/inference.v1.Gateway/Classify"

// GatewayClient is the client API for Gateway.
type GatewayClient interface {
	Classify(ctx context.Context, in *ClassifyRequest, opts ...grpc.CallOption) (*ClassifyResponse, error)
}

type gatewayClient struct{ cc grpc.ClientConnInterface }

func NewGatewayClient(cc grpc.ClientConnInterface) GatewayClient { return &gatewayClient{cc} }

func (c *gatewayClient) Classify(ctx context.Context, in *ClassifyRequest, opts ...grpc.CallOption) (*ClassifyResponse, error) {
	out := new(ClassifyResponse)
	if err := c.cc.Invoke(ctx, Gateway_Classify_FullMethodName, in, out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

// GatewayServer is the server API for Gateway.
type GatewayServer interface {
	Classify(context.Context, *ClassifyRequest) (*ClassifyResponse, error)
	mustEmbedUnimplementedGatewayServer()
}

// UnimplementedGatewayServer must be embedded for forward compatibility.
type UnimplementedGatewayServer struct{}

func (UnimplementedGatewayServer) Classify(context.Context, *ClassifyRequest) (*ClassifyResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method Classify not implemented")
}
func (UnimplementedGatewayServer) mustEmbedUnimplementedGatewayServer() {}

func RegisterGatewayServer(s grpc.ServiceRegistrar, srv GatewayServer) {
	s.RegisterService(&Gateway_ServiceDesc, srv)
}

func _Gateway_Classify_Handler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(ClassifyRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(GatewayServer).Classify(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: Gateway_Classify_FullMethodName}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(GatewayServer).Classify(ctx, req.(*ClassifyRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var Gateway_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "inference.v1.Gateway",
	HandlerType: (*GatewayServer)(nil),
	Methods:     []grpc.MethodDesc{{MethodName: "Classify", Handler: _Gateway_Classify_Handler}},
	Streams:     []grpc.StreamDesc{},
	Metadata:    "inference/v1/inference.proto",
}
