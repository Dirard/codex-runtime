package codex

import (
	"context"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
)

func (chat *Chat) RespondApproval(ctx context.Context, pendingRequestID string, decisionID string, opts ...RequestOption) (*pb.RespondChatPendingResponse, error) {
	return chat.respondPending(ctx, pendingRequestID, func(req *pb.RespondChatPendingRequest) {
		req.Response = &pb.RespondChatPendingRequest_Approval{
			Approval: &pb.ApprovalPendingResponse{DecisionId: decisionID},
		}
	}, opts...)
}

func (chat *Chat) RespondPermissions(ctx context.Context, pendingRequestID string, permissionIDs []string, scope pb.PermissionScope, strictAutoReview bool, opts ...RequestOption) (*pb.RespondChatPendingResponse, error) {
	return chat.respondPending(ctx, pendingRequestID, func(req *pb.RespondChatPendingRequest) {
		req.Response = &pb.RespondChatPendingRequest_Permissions{
			Permissions: &pb.PermissionsPendingResponse{
				PermissionIds:    append([]string(nil), permissionIDs...),
				Scope:            scope,
				StrictAutoReview: strictAutoReview,
			},
		}
	}, opts...)
}

func (chat *Chat) RespondMcpElicitation(ctx context.Context, pendingRequestID string, action pb.McpElicitationAction, contentJSON string, opts ...RequestOption) (*pb.RespondChatPendingResponse, error) {
	return chat.respondPending(ctx, pendingRequestID, func(req *pb.RespondChatPendingRequest) {
		req.Response = &pb.RespondChatPendingRequest_McpElicitation{
			McpElicitation: &pb.McpElicitationPendingResponse{
				Action:      action,
				ContentJson: contentJSON,
			},
		}
	}, opts...)
}

func (chat *Chat) RespondToolUserInput(ctx context.Context, pendingRequestID string, answers []*pb.ToolUserInputAnswer, opts ...RequestOption) (*pb.RespondChatPendingResponse, error) {
	return chat.respondPending(ctx, pendingRequestID, func(req *pb.RespondChatPendingRequest) {
		req.Response = &pb.RespondChatPendingRequest_ToolUserInput{
			ToolUserInput: &pb.ToolUserInputPendingResponse{
				Answers: append([]*pb.ToolUserInputAnswer(nil), answers...),
			},
		}
	}, opts...)
}

func (chat *Chat) respondPending(ctx context.Context, pendingRequestID string, applyResponse func(*pb.RespondChatPendingRequest), opts ...RequestOption) (*pb.RespondChatPendingResponse, error) {
	if err := chat.ready(); err != nil {
		return nil, err
	}
	applied := applyRequestOptions(opts)
	if err := applied.ensurePendingIDs(); err != nil {
		return nil, err
	}
	rpcCtx, err := chat.client.authenticatedContext(ctx)
	if err != nil {
		return nil, err
	}
	request := &pb.RespondChatPendingRequest{
		Context:          chat.client.runtimeContext(),
		ChatId:           chat.ID,
		PendingRequestId: pendingRequestID,
		ClientResponseId: applied.clientResponseID,
		IdempotencyKey:   applied.idempotencyKey,
	}
	if applyResponse != nil {
		applyResponse(request)
	}
	reply, err := chat.client.rpc.RespondChatPending(rpcCtx, request, chat.client.callOptions...)
	if err != nil {
		return nil, normalizeError(err)
	}
	chat.setStatus(reply.GetStatus())
	return reply, nil
}

func (chat *Chat) Interrupt(ctx context.Context, opts ...RequestOption) (*pb.InterruptChatRunResponse, error) {
	runID := ""
	if chat != nil {
		runID = chat.CachedStatus().GetCurrentRunId()
	}
	return chat.InterruptRun(ctx, runID, opts...)
}

func (chat *Chat) InterruptRun(ctx context.Context, runID string, opts ...RequestOption) (*pb.InterruptChatRunResponse, error) {
	if err := chat.ready(); err != nil {
		return nil, err
	}
	applied := applyRequestOptions(opts)
	if err := applied.ensureInterruptIDs(); err != nil {
		return nil, err
	}
	rpcCtx, err := chat.client.authenticatedContext(ctx)
	if err != nil {
		return nil, err
	}
	response, err := chat.client.rpc.InterruptChatRun(rpcCtx, &pb.InterruptChatRunRequest{
		Context:         chat.client.runtimeContext(),
		ChatId:          chat.ID,
		RunId:           runID,
		ClientRequestId: applied.clientRequestID,
		IdempotencyKey:  applied.idempotencyKey,
	}, chat.client.callOptions...)
	if err != nil {
		return nil, normalizeError(err)
	}
	chat.setStatus(response.GetStatus())
	return response, nil
}
