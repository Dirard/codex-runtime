package grpcapi

import (
	"testing"

	"github.com/Dirard/codex-runtime/internal/domain"
	"google.golang.org/grpc/codes"
)

func TestValidateStartTaskSourceURIRejectsEmptyQueryMarker(t *testing.T) {
	resolver := testResolver{
		"sg-1": testSessionGroupMetadata("sg-1", "ws-1"),
	}
	req := validStartTaskRequest()
	req.ContextBlocks[0].SourceUri = "https://example.com/source?"

	if _, err := ValidateStartTask(req, resolver); !hasRequestError(err, codes.InvalidArgument, domain.ReasonInvalidRequest) {
		t.Fatalf("ValidateStartTask() error = %#v, want invalid_request", err)
	}
}
