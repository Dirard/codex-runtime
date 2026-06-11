package grpcapi

import (
	"context"

	"github.com/Dirard/codex-runtime/internal/domain"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

const (
	authorizationMetadataKey = "authorization"
	bearerPrefix             = "Bearer "
)

func authUnaryInterceptor(expectedToken string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := authenticateContext(ctx, expectedToken); err != nil {
			return nil, err
		}
		return handler(sanitizedIncomingContext(ctx), req)
	}
}

func authStreamInterceptor(expectedToken string) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := authenticateContext(stream.Context(), expectedToken); err != nil {
			return err
		}
		return handler(srv, sanitizedServerStream{
			ServerStream: stream,
			ctx:          sanitizedIncomingContext(stream.Context()),
		})
	}
}

func authenticateContext(ctx context.Context, expectedToken string) error {
	metadataValues, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return unauthenticatedStatusError()
	}
	values := metadataValues.Get(authorizationMetadataKey)
	if len(values) != 1 {
		return unauthenticatedStatusError()
	}
	if values[0] != bearerPrefix+expectedToken {
		return unauthenticatedStatusError()
	}
	return nil
}

func sanitizedIncomingContext(ctx context.Context) context.Context {
	metadataValues, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}
	sanitizedMetadata := metadataValues.Copy()
	delete(sanitizedMetadata, authorizationMetadataKey)
	return metadata.NewIncomingContext(ctx, sanitizedMetadata)
}

type sanitizedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s sanitizedServerStream) Context() context.Context {
	return s.ctx
}

func unauthenticatedStatusError() error {
	return NewStatusError(codes.Unauthenticated, domain.GatewayErrorDetails{
		Reason:         domain.ReasonUnauthenticated,
		DisplayMessage: "unauthenticated",
	})
}
