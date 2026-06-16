package grpcapi

import (
	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
)

func validateInboundMessageForSession(message proto.Message, metadata domain.SessionGroupMetadata) *RequestError {
	if metadata.SessionGroupID == "" || metadata.GRPCInboundMessageBytes <= 0 || metadata.GRPCOutboundMessageBytes <= 0 {
		return internalGatewayRequestError()
	}
	if proto.Size(message) > metadata.GRPCInboundMessageBytes {
		return resourceExhausted("request exceeds session gRPC inbound message limit")
	}
	return nil
}

func validateInboundMessageForProcess(message proto.Message, maxBytes int) *RequestError {
	if maxBytes <= 0 {
		return internalGatewayRequestError()
	}
	if message != nil && proto.Size(message) > maxBytes {
		return resourceExhausted("request exceeds gateway gRPC inbound message limit")
	}
	return nil
}

func validateInboundMessageForKnownLocator(message proto.Message, locator domain.TaskLocator, resolver SessionGroupResolver) *RequestError {
	metadata, ok, requestError := trustedSessionMetadataForKnownLocator(locator, resolver)
	if requestError != nil {
		return requestError
	}
	if !ok {
		return nil
	}
	return validateInboundMessageForSession(message, metadata)
}

func validateOutboundMessageForSession(message proto.Message, resolver SessionGroupResolver, sessionGroupID string) error {
	if sessionGroupID == "" {
		return redactedInternalStatusError()
	}
	metadata, ok := resolveSessionGroup(resolver, sessionGroupID)
	if !ok {
		return redactedInternalStatusError()
	}
	return validateOutboundMessageForTrustedSession(message, metadata, sessionGroupID)
}

func validateOutboundMessageForTrustedSession(message proto.Message, metadata domain.SessionGroupMetadata, responseSessionGroupID string) error {
	if metadata.SessionGroupID == "" ||
		metadata.GRPCInboundMessageBytes <= 0 ||
		metadata.GRPCOutboundMessageBytes <= 0 ||
		responseSessionGroupID != metadata.SessionGroupID {
		return redactedInternalStatusError()
	}
	if proto.Size(message) > metadata.GRPCOutboundMessageBytes {
		return NewStatusError(codes.ResourceExhausted, domain.GatewayErrorDetails{
			Reason:         domain.ReasonResourceExhausted,
			DisplayMessage: "response exceeds session gRPC outbound message limit",
		})
	}
	return nil
}

func validateOutboundMessageForProcess(message proto.Message, maxBytes int) error {
	if maxBytes <= 0 {
		return redactedInternalStatusError()
	}
	if message != nil && proto.Size(message) > maxBytes {
		return NewStatusError(codes.ResourceExhausted, domain.GatewayErrorDetails{
			Reason:         domain.ReasonResourceExhausted,
			DisplayMessage: "response exceeds gateway gRPC outbound message limit",
		})
	}
	return nil
}

func trustedSessionMetadataForKnownLocator(locator domain.TaskLocator, resolver SessionGroupResolver) (domain.SessionGroupMetadata, bool, *RequestError) {
	sessionGroupID := locatorSessionGroupID(locator)
	if sessionGroupID == "" {
		return domain.SessionGroupMetadata{}, false, nil
	}
	metadata, requestError := trustedSessionMetadataForSessionGroupID(sessionGroupID, resolver)
	if requestError != nil {
		return domain.SessionGroupMetadata{}, false, requestError
	}
	return metadata, true, nil
}

func trustedSessionMetadataForSessionGroupID(sessionGroupID string, resolver SessionGroupResolver) (domain.SessionGroupMetadata, *RequestError) {
	metadata, ok := resolveSessionGroup(resolver, sessionGroupID)
	if !ok {
		return domain.SessionGroupMetadata{}, notFound(domain.ReasonUnknownSessionGroup, "unknown session group")
	}
	if metadata.SessionGroupID != sessionGroupID ||
		metadata.GRPCInboundMessageBytes <= 0 ||
		metadata.GRPCOutboundMessageBytes <= 0 {
		return domain.SessionGroupMetadata{}, internalGatewayRequestError()
	}
	return metadata, nil
}

func locatorSessionGroupID(locator domain.TaskLocator) string {
	switch locator.Kind {
	case domain.TaskLocatorByClientMessage:
		return locator.ClientMessageLocator.SessionGroupID
	case domain.TaskLocatorByThread:
		return locator.ThreadLocator.SessionGroupID
	case domain.TaskLocatorByTaskID:
		return ""
	default:
		return ""
	}
}
