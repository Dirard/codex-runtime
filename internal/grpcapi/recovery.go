package grpcapi

import (
	"context"

	"github.com/Dirard/codex-runtime/internal/domain"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func recoveryUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (response any, err error) {
		defer func() {
			if recover() != nil {
				response = nil
				err = redactedInternalStatusError()
			}
		}()
		return handler(ctx, req)
	}
}

func recoveryStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if recover() != nil {
				err = redactedInternalStatusError()
			}
		}()
		return handler(srv, stream)
	}
}

func redactedInternalStatusError() error {
	return NewStatusError(codes.Internal, domain.GatewayErrorDetails{
		Reason:         domain.ReasonInternalGatewayError,
		DisplayMessage: "internal gateway error",
	})
}
