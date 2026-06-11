package domain_test

import (
	"testing"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"github.com/Dirard/codex-runtime/internal/domain"
	"github.com/Dirard/codex-runtime/internal/grpcapi"
)

type fingerprintTestResolver map[string]domain.SessionGroupMetadata

func (r fingerprintTestResolver) ResolveSessionGroup(sessionGroupID string) (domain.SessionGroupMetadata, bool) {
	metadata, ok := r[sessionGroupID]
	return metadata, ok
}

func TestStartTaskFingerprintV1StableForAbsentAndMatchingWorkspaceID(t *testing.T) {
	resolver := fingerprintTestResolver{
		"sg-1": {
			SessionGroupID:           "sg-1",
			WorkspaceID:              "ws-1",
			GRPCInboundMessageBytes:  4 * domain.MiB,
			GRPCOutboundMessageBytes: 4 * domain.MiB,
		},
	}
	withoutWorkspace := &pb.StartTaskRequest{
		SessionGroupId:  "sg-1",
		Prompt:          "hello",
		ClientMessageId: "client-message-1",
	}
	withMatchingWorkspace := &pb.StartTaskRequest{
		SessionGroupId:  "sg-1",
		WorkspaceId:     "ws-1",
		Prompt:          "hello",
		ClientMessageId: "client-message-1",
	}

	withoutCommand, requestErr := grpcapi.ValidateStartTask(withoutWorkspace, resolver)
	if requestErr != nil {
		t.Fatalf("ValidateStartTask(without workspace) error = %v", requestErr)
	}
	withCommand, requestErr := grpcapi.ValidateStartTask(withMatchingWorkspace, resolver)
	if requestErr != nil {
		t.Fatalf("ValidateStartTask(with matching workspace) error = %v", requestErr)
	}

	withoutDigest, err := domain.StartTaskFingerprintV1SHA256Hex(withoutCommand)
	if err != nil {
		t.Fatalf("StartTaskFingerprintV1SHA256Hex(without workspace) error = %v", err)
	}
	withDigest, err := domain.StartTaskFingerprintV1SHA256Hex(withCommand)
	if err != nil {
		t.Fatalf("StartTaskFingerprintV1SHA256Hex(with matching workspace) error = %v", err)
	}
	if withoutDigest != withDigest {
		t.Fatalf("digest changed for absent vs matching workspace_id: %s != %s", withoutDigest, withDigest)
	}
}
