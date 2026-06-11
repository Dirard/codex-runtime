package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"github.com/Dirard/codex-runtime/internal/config"
	"github.com/Dirard/codex-runtime/internal/domain"
	"github.com/Dirard/codex-runtime/internal/grpcapi"
	"github.com/Dirard/codex-runtime/internal/testappserver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const grpcE2EToken = "fixture-token-stage-10"

func TestGRPCE2EStartStreamPendingInterruptAndStatus(t *testing.T) {
	const (
		clientMessageID = "client-message-1"
		prompt          = "approve and interrupt"
	)
	group := testSessionGroup(t, "sg-1", "ws-1")
	group.GRPCLimits = config.GRPCLimits{
		InboundMessageBytes:  4 * domain.MiB,
		OutboundMessageBytes: 4 * domain.MiB,
	}
	checkTurnStart := testappserver.CheckMessage(func(message testappserver.Message) error {
		var params struct {
			ThreadID            string `json:"threadId"`
			ClientUserMessageID string `json:"clientUserMessageId"`
			Input               []struct {
				Type         string `json:"type"`
				Text         string `json:"text"`
				TextElements []any  `json:"text_elements"`
			} `json:"input"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return err
		}
		if params.ThreadID != "thread-1" {
			return fmt.Errorf("turn/start threadId = %q, want thread-1", params.ThreadID)
		}
		if params.ClientUserMessageID != clientMessageID {
			return fmt.Errorf("turn/start clientUserMessageId = %q, want %q", params.ClientUserMessageID, clientMessageID)
		}
		if len(params.Input) != 1 {
			return fmt.Errorf("turn/start input length = %d, want 1", len(params.Input))
		}
		input := params.Input[0]
		if input.Type != "text" || len(input.TextElements) != 0 {
			return fmt.Errorf("turn/start input = %#v, want UserInput.Text with empty text_elements", input)
		}
		_, envelopeJSON, ok := strings.Cut(input.Text, "\n")
		if !ok {
			return errors.New("turn/start text is missing envelope payload")
		}
		var envelope struct {
			UserPrompt string `json:"userPrompt"`
		}
		if err := json.Unmarshal([]byte(envelopeJSON), &envelope); err != nil {
			return fmt.Errorf("turn/start envelope payload: %w", err)
		}
		if envelope.UserPrompt != prompt {
			return fmt.Errorf("turn/start envelope userPrompt = %q, want %q", envelope.UserPrompt, prompt)
		}
		return nil
	})
	checkTurnInterrupt := testappserver.CheckMessage(func(message testappserver.Message) error {
		var params struct {
			TurnID string `json:"turnId"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return err
		}
		if params.TurnID != "turn-1" {
			return fmt.Errorf("turn/interrupt turnId = %q, want turn-1", params.TurnID)
		}
		return nil
	})
	steps := append([]testappserver.Step{}, testappserver.ThreadStart("thread-1")...)
	steps = append(steps,
		testappserver.ExpectRequest(testappserver.MethodTurnStart, testappserver.CaptureID(testappserver.MethodTurnStart), checkTurnStart),
		testappserver.SendNotification(testappserver.MethodTurnStarted, testappserver.TurnStartedParams("thread-1", "turn-1")),
		testappserver.SendResponseFor(testappserver.MethodTurnStart, testappserver.TurnResult("turn-1", "running")),
		testappserver.SendCommandApprovalRequest(101, "thread-1", "turn-1", "item-command"),
		testappserver.ExpectResponseID(101, testappserver.WithResult(map[string]any{"decision": "accept"})),
		testappserver.ExpectRequest(testappserver.MethodTurnInterrupt, testappserver.CaptureID(testappserver.MethodTurnInterrupt), checkTurnInterrupt),
		testappserver.SendResponseFor(testappserver.MethodTurnInterrupt, testappserver.TurnResult("turn-1", "interrupted")),
		testappserver.SendNotification(testappserver.MethodTurnCompleted, testappserver.TurnCompletedParams("thread-1", "turn-1", "interrupted")),
	)
	service, appServer := newHarnessService(t, group, steps...)
	harness := startGRPCE2EHarness(t, group, service)

	start, err := harness.client.StartTask(grpcE2EContext(context.Background()), &pb.StartTaskRequest{
		SessionGroupId:  group.SessionGroupID,
		WorkspaceId:     group.WorkspaceID,
		Prompt:          prompt,
		ClientMessageId: clientMessageID,
	})
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	if start.GetTaskId() == "" || start.GetThreadId() != "thread-1" || start.GetTurnId() != "turn-1" ||
		start.GetSessionGroupId() != group.SessionGroupID ||
		(start.GetState() != pb.TaskState_TASK_STATE_RUNNING &&
			start.GetState() != pb.TaskState_TASK_STATE_WAITING_FOR_PENDING_REQUEST) ||
		start.GetLastEventId() == 0 {
		t.Fatalf("StartTask() response = %#v", start)
	}

	statusByTask := waitGRPCStatusTaskID(t, harness.client, taskIDLocator(start.GetTaskId()), start.GetTaskId())
	statusByClient := waitGRPCStatusTaskID(
		t,
		harness.client,
		clientMessageLocator(group.SessionGroupID, clientMessageID),
		start.GetTaskId(),
	)
	if statusByClient.GetTaskId() != start.GetTaskId() || statusByTask.GetTaskId() != start.GetTaskId() {
		t.Fatalf("status recovery task ids = task:%q client:%q start:%q", statusByTask.GetTaskId(), statusByClient.GetTaskId(), start.GetTaskId())
	}

	replayStatus := waitGRPCStatusAtLeastEvent(t, harness.client, start.GetTaskId(), start.GetLastEventId())
	replayCtx, cancelReplay := context.WithTimeout(grpcE2EContext(context.Background()), 2*time.Second)
	replay, err := harness.client.StreamTask(replayCtx, &pb.StreamTaskRequest{
		TaskId: start.GetTaskId(),
		Cursor: &pb.StreamTaskRequest_FromStart{
			FromStart: &pb.FromStartCursor{},
		},
		ClientSubscriberId: "replay-subscriber",
	})
	if err != nil {
		cancelReplay()
		t.Fatalf("StreamTask(from_start) error = %v", err)
	}
	replayed := recvReplayThrough(t, replay, replayStatus.GetNewestBufferedEventId())
	cancelReplay()
	waitSubscriberCount(t, service, start.GetTaskId(), 0)
	if replayed[0].GetEventId() != 1 || replayed[len(replayed)-1].GetEventId() < start.GetLastEventId() {
		t.Fatalf("replayed events = %#v, want replay from event 1 through start event %d", replayed, start.GetLastEventId())
	}

	pendingStatus := waitGRPCPendingCount(t, harness.client, start.GetTaskId(), 1)
	pendingRequest := pendingStatus.GetActivePendingRequests()[0]
	invalidCtx, cancelInvalid := context.WithTimeout(grpcE2EContext(context.Background()), time.Second)
	invalidStream, err := harness.client.StreamTask(invalidCtx, &pb.StreamTaskRequest{
		TaskId: start.GetTaskId(),
		Cursor: &pb.StreamTaskRequest_AfterEventId{
			AfterEventId: pendingStatus.GetNewestBufferedEventId() + 1,
		},
		ClientSubscriberId: "invalid-cursor-subscriber",
	})
	if err == nil {
		_, err = invalidStream.Recv()
	}
	cancelInvalid()
	assertGRPCStatus(t, err, codes.InvalidArgument, domain.ReasonInvalidCursor)

	liveCtx, cancelLive := context.WithTimeout(grpcE2EContext(context.Background()), 2*time.Second)
	live, err := harness.client.StreamTask(liveCtx, &pb.StreamTaskRequest{
		TaskId: start.GetTaskId(),
		Cursor: &pb.StreamTaskRequest_AfterEventId{
			AfterEventId: pendingStatus.GetNewestBufferedEventId(),
		},
		ClientSubscriberId: "live-subscriber",
	})
	if err != nil {
		cancelLive()
		t.Fatalf("StreamTask(live) error = %v", err)
	}

	respondRequest := &pb.RespondPendingRequestRequest{
		TaskId:           start.GetTaskId(),
		PendingRequestId: pendingRequest.GetPendingRequestId(),
		ClientResponseId: "client-response-1",
		Response: &pb.RespondPendingRequestRequest_Approval{
			Approval: &pb.ApprovalPendingResponse{DecisionId: "decision-accept"},
		},
	}
	respond, err := harness.client.RespondPendingRequest(grpcE2EContext(context.Background()), respondRequest)
	if err != nil {
		cancelLive()
		t.Fatalf("RespondPendingRequest() error = %v", err)
	}
	if !respond.GetAccepted() || respond.GetAlreadyApplied() || respond.GetResolvedEventId() == 0 {
		cancelLive()
		t.Fatalf("RespondPendingRequest() response = %#v", respond)
	}
	liveEvent := recvStreamEvent(t, live)
	cancelLive()
	waitSubscriberCount(t, service, start.GetTaskId(), 0)
	if liveEvent.GetPendingRequestResolved() == nil ||
		liveEvent.GetPendingRequestResolved().GetPendingRequestId() != pendingRequest.GetPendingRequestId() ||
		liveEvent.GetEventId() != respond.GetResolvedEventId() {
		t.Fatalf("live event = %#v, want pending resolution event %d", liveEvent, respond.GetResolvedEventId())
	}

	retry, err := harness.client.RespondPendingRequest(grpcE2EContext(context.Background()), respondRequest)
	if err != nil {
		t.Fatalf("RespondPendingRequest() retry error = %v", err)
	}
	if !retry.GetAccepted() || !retry.GetAlreadyApplied() || retry.GetResolvedEventId() != respond.GetResolvedEventId() {
		t.Fatalf("RespondPendingRequest() retry = %#v, want already-applied event %d", retry, respond.GetResolvedEventId())
	}
	if got := countOutboundResponsesWithID(appServer.OutboundMessages(), "101"); got != 1 {
		t.Fatalf("app-server response writes for request 101 = %d, want 1", got)
	}

	interrupt, err := harness.client.InterruptTask(grpcE2EContext(context.Background()), &pb.InterruptTaskRequest{
		Locator: &pb.InterruptTaskRequest_TaskId{
			TaskId: start.GetTaskId(),
		},
		ClientRequestId: "interrupt-1",
	})
	if err != nil {
		t.Fatalf("InterruptTask() error = %v", err)
	}
	if !interrupt.GetInterruptSent() || interrupt.GetTaskId() != start.GetTaskId() ||
		(interrupt.GetState() != pb.TaskState_TASK_STATE_INTERRUPTING &&
			interrupt.GetState() != pb.TaskState_TASK_STATE_INTERRUPTED) {
		t.Fatalf("InterruptTask() response = %#v", interrupt)
	}

	terminalStatus := waitGRPCStatusState(t, harness.client, taskIDLocator(start.GetTaskId()), pb.TaskState_TASK_STATE_INTERRUPTED)
	terminalClientStatus := waitGRPCStatusState(t, harness.client, clientMessageLocator(group.SessionGroupID, clientMessageID), pb.TaskState_TASK_STATE_INTERRUPTED)
	if terminalClientStatus.GetTaskId() != terminalStatus.GetTaskId() {
		t.Fatalf("terminal status by client task_id = %q, want %q", terminalClientStatus.GetTaskId(), terminalStatus.GetTaskId())
	}

	terminalCtx, cancelTerminal := context.WithTimeout(grpcE2EContext(context.Background()), time.Second)
	terminalStream, err := harness.client.StreamTask(terminalCtx, &pb.StreamTaskRequest{
		TaskId: start.GetTaskId(),
		Cursor: &pb.StreamTaskRequest_AfterEventId{
			AfterEventId: terminalStatus.GetNewestBufferedEventId(),
		},
		ClientSubscriberId: "terminal-subscriber",
	})
	if err != nil {
		cancelTerminal()
		t.Fatalf("StreamTask(terminal newest) error = %v", err)
	}
	_, err = terminalStream.Recv()
	cancelTerminal()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("StreamTask(terminal newest) Recv() error = %v, want EOF", err)
	}

	appServer.RequireDone(t)
}

type grpcE2EHarness struct {
	client pb.CodexControlClient
}

func startGRPCE2EHarness(t *testing.T, group config.SessionGroup, service *Service) *grpcE2EHarness {
	t.Helper()
	server, err := grpcapi.NewServer(grpcapi.ServerOptions{
		ListenAddress:       "127.0.0.1:0",
		AuthToken:           grpcE2EToken,
		MaxRecvMessageBytes: int(group.GRPCLimits.InboundMessageBytes),
		MaxSendMessageBytes: int(group.GRPCLimits.OutboundMessageBytes),
		Services: grpcapi.ControlServices{
			SessionGroups: grpcapi.SessionGroupResolverFunc(func(sessionGroupID string) (domain.SessionGroupMetadata, bool) {
				if sessionGroupID != group.SessionGroupID {
					return domain.SessionGroupMetadata{}, false
				}
				return domain.SessionGroupMetadata{
					SessionGroupID:           group.SessionGroupID,
					WorkspaceID:              group.WorkspaceID,
					GRPCInboundMessageBytes:  int(group.GRPCLimits.InboundMessageBytes),
					GRPCOutboundMessageBytes: int(group.GRPCLimits.OutboundMessageBytes),
				}, true
			}),
			Tasks:   service,
			Pending: service,
		},
	})
	if err != nil {
		t.Fatalf("grpcapi.NewServer() error = %v", err)
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve()
	}()
	conn, err := grpc.NewClient(server.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		server.Stop()
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		server.Stop()
		select {
		case err := <-serveErr:
			if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
				t.Errorf("Serve() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Error("Serve() did not stop within timeout")
		}
	})
	return &grpcE2EHarness{client: pb.NewCodexControlClient(conn)}
}

func grpcE2EContext(ctx context.Context) context.Context {
	return metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+grpcE2EToken))
}

func taskIDLocator(taskID string) *pb.GetTaskStatusRequest {
	return &pb.GetTaskStatusRequest{
		Locator: &pb.GetTaskStatusRequest_TaskId{
			TaskId: taskID,
		},
	}
}

func clientMessageLocator(sessionGroupID string, clientMessageID string) *pb.GetTaskStatusRequest {
	return &pb.GetTaskStatusRequest{
		Locator: &pb.GetTaskStatusRequest_ClientMessageLocator{
			ClientMessageLocator: &pb.ClientMessageTaskLocator{
				SessionGroupId:  sessionGroupID,
				ClientMessageId: clientMessageID,
			},
		},
	}
}

func waitGRPCStatusState(
	t *testing.T,
	client pb.CodexControlClient,
	request *pb.GetTaskStatusRequest,
	state pb.TaskState,
) *pb.GetTaskStatusResponse {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last *pb.GetTaskStatusResponse
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(grpcE2EContext(context.Background()), 200*time.Millisecond)
		status, err := client.GetTaskStatus(ctx, request)
		cancel()
		if err == nil {
			last = status
			if status.GetState() == state {
				return status
			}
		} else {
			lastErr = err
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("GetTaskStatus() did not reach state %s; last status=%#v err=%v", state, last, lastErr)
	return nil
}

func waitGRPCStatusTaskID(
	t *testing.T,
	client pb.CodexControlClient,
	request *pb.GetTaskStatusRequest,
	taskID string,
) *pb.GetTaskStatusResponse {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last *pb.GetTaskStatusResponse
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(grpcE2EContext(context.Background()), 200*time.Millisecond)
		status, err := client.GetTaskStatus(ctx, request)
		cancel()
		if err == nil {
			last = status
			if status.GetTaskId() == taskID {
				return status
			}
		} else {
			lastErr = err
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("GetTaskStatus() did not recover task %q; last status=%#v err=%v", taskID, last, lastErr)
	return nil
}

func waitGRPCStatusAtLeastEvent(
	t *testing.T,
	client pb.CodexControlClient,
	taskID string,
	eventID uint64,
) *pb.GetTaskStatusResponse {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last *pb.GetTaskStatusResponse
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(grpcE2EContext(context.Background()), 200*time.Millisecond)
		status, err := client.GetTaskStatus(ctx, taskIDLocator(taskID))
		cancel()
		if err == nil {
			last = status
			if status.GetNewestBufferedEventId() >= eventID {
				return status
			}
		} else {
			lastErr = err
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("GetTaskStatus() newest event did not reach %d; last status=%#v err=%v", eventID, last, lastErr)
	return nil
}

func waitGRPCPendingCount(
	t *testing.T,
	client pb.CodexControlClient,
	taskID string,
	count int,
) *pb.GetTaskStatusResponse {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last *pb.GetTaskStatusResponse
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(grpcE2EContext(context.Background()), 200*time.Millisecond)
		status, err := client.GetTaskStatus(ctx, taskIDLocator(taskID))
		cancel()
		if err == nil {
			last = status
			if len(status.GetActivePendingRequests()) == count {
				return status
			}
		} else {
			lastErr = err
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("GetTaskStatus() pending count did not reach %d; last status=%#v err=%v", count, last, lastErr)
	return nil
}

func recvReplayThrough(t *testing.T, stream pb.CodexControl_StreamTaskClient, newestEventID uint64) []*pb.TaskEvent {
	t.Helper()
	var events []*pb.TaskEvent
	for {
		event := recvStreamEvent(t, stream)
		events = append(events, event)
		if event.GetEventId() >= newestEventID {
			return events
		}
	}
}

func recvStreamEvent(t *testing.T, stream pb.CodexControl_StreamTaskClient) *pb.TaskEvent {
	t.Helper()
	for {
		message, err := stream.Recv()
		if err != nil {
			t.Fatalf("StreamTask().Recv() error = %v", err)
		}
		if event := message.GetEvent(); event != nil {
			return event
		}
	}
}

func waitSubscriberCount(t *testing.T, service *Service, taskID string, count int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		service.mu.Lock()
		task := service.tasks[taskID]
		got := -1
		if task != nil {
			got = len(task.subscribers)
		}
		service.mu.Unlock()
		if got == count {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	service.mu.Lock()
	task := service.tasks[taskID]
	got := -1
	if task != nil {
		got = len(task.subscribers)
	}
	service.mu.Unlock()
	t.Fatalf("subscriber count = %d, want %d", got, count)
}

func assertGRPCStatus(t *testing.T, err error, code codes.Code, reason domain.GatewayErrorReason) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want code %s reason %s", code, reason)
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("error = %T %v, want gRPC status", err, err)
	}
	if st.Code() != code {
		t.Fatalf("status code = %s, want %s", st.Code(), code)
	}
	for _, detail := range st.Details() {
		gatewayDetails, ok := detail.(*pb.GatewayErrorDetails)
		if !ok {
			continue
		}
		if gatewayDetails.GetReason() != string(reason) {
			t.Fatalf("status reason = %q, want %q", gatewayDetails.GetReason(), reason)
		}
		return
	}
	t.Fatalf("status details = %#v, want GatewayErrorDetails reason %s", st.Details(), reason)
}

func countOutboundResponsesWithID(messages []testappserver.Message, id string) int {
	count := 0
	for _, message := range messages {
		if message.Method == "" && message.Error == nil && len(message.Result) > 0 && string(message.ID) == id {
			count++
		}
	}
	return count
}
