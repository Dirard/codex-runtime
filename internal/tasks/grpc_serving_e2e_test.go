package tasks

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"github.com/Dirard/codex-runtime/internal/config"
	"github.com/Dirard/codex-runtime/internal/domain"
	"github.com/Dirard/codex-runtime/internal/testappserver"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGRPCE2EStartTaskCanceledCallerCanRecoverAndContinue(t *testing.T) {
	const clientMessageID = "client-message-recover"
	group := grpcServingE2EGroup(t, "sg-recover", "ws-recover")
	threadStartGate := newGRPCScriptGate(t, "recover thread/start response")
	defer threadStartGate.release()
	steps := []testappserver.Step{
		testappserver.ExpectRequest(testappserver.MethodThreadStart, testappserver.CaptureID(testappserver.MethodThreadStart)),
		threadStartGate.step(),
		testappserver.SendNotification(testappserver.MethodThreadStarted, testappserver.ThreadStartedParams("thread-recover")),
		testappserver.SendResponseFor(testappserver.MethodThreadStart, testappserver.ThreadResult("thread-recover")),
	}
	steps = append(steps, testappserver.TurnStart("thread-recover", "turn-recover")...)
	steps = append(steps,
		testappserver.SendCommandApprovalRequest(101, "thread-recover", "turn-recover", "item-command"),
		testappserver.ExpectResponseID(101, testappserver.WithResult(map[string]any{"decision": "accept"})),
	)
	service, appServer := newHarnessService(t, group, steps...)
	harness := startGRPCE2EHarness(t, group, service)
	ctx, cancel := context.WithCancel(grpcE2EContext(context.Background()))
	start := startGRPCAsyncContext(harness.client, ctx, startTaskRequest(group, clientMessageID, "recover after lost response"))
	appServer.RequireOutboundRequest(t, 2, testappserver.MethodThreadStart)

	cancel()
	_, err := start.wait(t)
	assertGRPCCode(t, err, codes.Canceled)
	threadStartGate.release()

	recovered := waitGRPCPendingCountByClient(t, harness.client, group.SessionGroupID, clientMessageID, 1)
	taskID := recovered.GetTaskId()
	if taskID == "" || recovered.GetThreadId() != "thread-recover" || recovered.GetTurnId() != "turn-recover" {
		t.Fatalf("recovered status = %#v", recovered)
	}

	replayCtx, cancelReplay := context.WithTimeout(grpcE2EContext(context.Background()), time.Second)
	replay, err := harness.client.StreamTask(replayCtx, &pb.StreamTaskRequest{
		TaskId: taskID,
		Cursor: &pb.StreamTaskRequest_FromStart{
			FromStart: &pb.FromStartCursor{},
		},
		ClientSubscriberId: "recovered-replay",
	})
	if err != nil {
		cancelReplay()
		t.Fatalf("StreamTask(recovered task) error = %v", err)
	}
	events := recvReplayThrough(t, replay, recovered.GetNewestBufferedEventId())
	cancelReplay()
	waitSubscriberCount(t, service, taskID, 0)
	if len(events) == 0 || events[len(events)-1].GetEventId() != recovered.GetNewestBufferedEventId() {
		t.Fatalf("recovered replay events = %#v, status = %#v", events, recovered)
	}

	pendingRequest := recovered.GetActivePendingRequests()[0]
	respond, err := harness.client.RespondPendingRequest(grpcE2EContext(context.Background()), &pb.RespondPendingRequestRequest{
		TaskId:           taskID,
		PendingRequestId: pendingRequest.GetPendingRequestId(),
		ClientResponseId: "client-response-recover",
		Response: &pb.RespondPendingRequestRequest_Approval{
			Approval: &pb.ApprovalPendingResponse{DecisionId: "decision-accept"},
		},
	})
	if err != nil {
		t.Fatalf("RespondPendingRequest(recovered task) error = %v", err)
	}
	if !respond.GetAccepted() || respond.GetTaskId() != taskID || respond.GetResolvedEventId() == 0 {
		t.Fatalf("RespondPendingRequest(recovered task) response = %#v", respond)
	}
	if got := countOutboundResponsesWithID(appServer.OutboundMessages(), "101"); got != 1 {
		t.Fatalf("app-server response writes for request 101 = %d, want 1", got)
	}
	appServer.RequireDone(t)
}

func TestGRPCE2EStreamTaskReplayEvictionNotices(t *testing.T) {
	group := grpcServingE2EGroup(t, "sg-replay", "ws-replay")
	group.ReplayLimits.MaxEvents = 3
	group.ReplayLimits.MaxBytes = 1 << 20
	service, appServer := newHarnessService(t, group,
		append(testappserver.ThreadStart("thread-replay"), testappserver.TurnStart("thread-replay", "turn-replay")...)...,
	)
	harness := startGRPCE2EHarness(t, group, service)
	start, err := harness.client.StartTask(
		grpcE2EContext(context.Background()),
		startTaskRequest(group, "client-message-replay", "evict replay"),
	)
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	for i := 0; i < 5; i++ {
		appendGatewayWarningForTest(service, start.GetTaskId(), "grpc-replay-eviction")
	}
	appendTerminalForTest(service, start.GetTaskId(), domain.TerminalStateCompleted)

	replayStatus := waitGRPCStatusState(t, harness.client, taskIDLocator(start.GetTaskId()), pb.TaskState_TASK_STATE_COMPLETED)
	if replayStatus.GetFromStartAvailable() ||
		replayStatus.GetOldestBufferedEventId() <= 1 ||
		replayStatus.GetNewestBufferedEventId() != replayStatus.GetLastEventId() {
		t.Fatalf("replay status = %#v", replayStatus)
	}

	fromStart := openGRPCStream(t, harness.client, &pb.StreamTaskRequest{
		TaskId: start.GetTaskId(),
		Cursor: &pb.StreamTaskRequest_FromStart{
			FromStart: &pb.FromStartCursor{},
		},
		ClientSubscriberId: "from-start-evicted",
	})
	startNotice := recvStreamNotice(t, fromStart)
	assertReplayNotice(t, startNotice, pb.ReplayNoticeCode_REPLAY_NOTICE_CODE_START_EVICTED, replayStatus)
	firstFromStart := recvStreamEvent(t, fromStart)
	if firstFromStart.GetEventId() != replayStatus.GetOldestBufferedEventId() {
		t.Fatalf("from_start first replay event = %#v, want event id %d", firstFromStart, replayStatus.GetOldestBufferedEventId())
	}

	afterEvicted := openGRPCStream(t, harness.client, &pb.StreamTaskRequest{
		TaskId: start.GetTaskId(),
		Cursor: &pb.StreamTaskRequest_AfterEventId{
			AfterEventId: 1,
		},
		ClientSubscriberId: "after-evicted",
	})
	cursorNotice := recvStreamNotice(t, afterEvicted)
	assertReplayNotice(t, cursorNotice, pb.ReplayNoticeCode_REPLAY_NOTICE_CODE_CURSOR_EVICTED, replayStatus)
	firstAfterCursor := recvStreamEvent(t, afterEvicted)
	if firstAfterCursor.GetEventId() != replayStatus.GetOldestBufferedEventId() {
		t.Fatalf("after_event_id first replay event = %#v, want event id %d", firstAfterCursor, replayStatus.GetOldestBufferedEventId())
	}

	appServer.RequireDone(t)
}

func TestGRPCE2EPreTurnInterruptByClientMessageCancelsStart(t *testing.T) {
	const clientMessageID = "client-message-pre-turn"
	group := grpcServingE2EGroup(t, "sg-pre-turn", "ws-pre-turn")
	threadStartGate := newGRPCScriptGate(t, "pre-turn thread/start response")
	defer threadStartGate.release()
	service, appServer := newHarnessService(t, group,
		testappserver.ExpectRequest(testappserver.MethodThreadStart, testappserver.CaptureID(testappserver.MethodThreadStart)),
		threadStartGate.step(),
		testappserver.SendNotification(testappserver.MethodThreadStarted, testappserver.ThreadStartedParams("thread-pre-turn")),
		testappserver.SendResponseFor(testappserver.MethodThreadStart, testappserver.ThreadResult("thread-pre-turn")),
	)
	harness := startGRPCE2EHarness(t, group, service)
	start := startGRPCAsync(harness.client, startTaskRequest(group, clientMessageID, "interrupt before turn"))
	appServer.RequireOutboundRequest(t, 2, testappserver.MethodThreadStart)

	interrupt, err := harness.client.InterruptTask(grpcE2EContext(context.Background()), &pb.InterruptTaskRequest{
		Locator: &pb.InterruptTaskRequest_ClientMessageLocator{
			ClientMessageLocator: &pb.ClientMessageTaskLocator{
				SessionGroupId:  group.SessionGroupID,
				ClientMessageId: clientMessageID,
			},
		},
		ClientRequestId: "interrupt-pre-turn",
	})
	if err != nil {
		t.Fatalf("InterruptTask(client message) error = %v", err)
	}
	if !interrupt.GetPreTurnCancelRecorded() || interrupt.GetTaskId() == "" {
		t.Fatalf("InterruptTask(client message) response = %#v", interrupt)
	}
	threadStartGate.release()
	_, err = start.wait(t)
	assertGRPCStatus(t, err, codes.Canceled, domain.ReasonStartInterruptedBeforeTurn)

	status := waitGRPCStatusState(
		t,
		harness.client,
		clientMessageLocator(group.SessionGroupID, clientMessageID),
		pb.TaskState_TASK_STATE_INTERRUPTED,
	)
	if status.GetTaskId() != interrupt.GetTaskId() || status.GetThreadId() != "thread-pre-turn" || status.GetTurnId() != "" {
		t.Fatalf("status after pre-turn interrupt = %#v, interrupt = %#v", status, interrupt)
	}
	appServer.RequireDone(t)
	if got := countOutboundMethod(appServer.OutboundMessages(), testappserver.MethodTurnStart); got != 0 {
		t.Fatalf("turn/start calls = %d, want 0", got)
	}
}

func TestGRPCE2EConcurrentCallsStayResponsiveDuringRunningTask(t *testing.T) {
	group := grpcServingE2EGroup(t, "sg-concurrent", "ws-concurrent")
	interruptGate := newGRPCScriptGate(t, "turn/interrupt response")
	defer interruptGate.release()
	steps := append([]testappserver.Step{}, testappserver.ThreadStart("thread-concurrent")...)
	steps = append(steps, testappserver.TurnStart("thread-concurrent", "turn-concurrent")...)
	steps = append(steps,
		testappserver.SendCommandApprovalRequest(101, "thread-concurrent", "turn-concurrent", "item-command"),
		testappserver.ExpectResponseID(101, testappserver.WithResult(map[string]any{"decision": "accept"})),
		testappserver.ExpectRequest(testappserver.MethodTurnInterrupt, testappserver.CaptureID(testappserver.MethodTurnInterrupt)),
		interruptGate.step(),
		testappserver.SendResponseFor(testappserver.MethodTurnInterrupt, testappserver.TurnResult("turn-concurrent", "interrupted")),
		testappserver.SendNotification(testappserver.MethodTurnCompleted, testappserver.TurnCompletedParams("thread-concurrent", "turn-concurrent", "interrupted")),
	)
	service, appServer := newHarnessService(t, group, steps...)
	harness := startGRPCE2EHarness(t, group, service)
	start, err := harness.client.StartTask(
		grpcE2EContext(context.Background()),
		startTaskRequest(group, "client-message-concurrent", "stay responsive"),
	)
	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	pendingStatus := waitGRPCPendingCount(t, harness.client, start.GetTaskId(), 1)
	liveCtx, cancelLive := context.WithTimeout(grpcE2EContext(context.Background()), 2*time.Second)
	defer cancelLive()
	live, err := harness.client.StreamTask(liveCtx, &pb.StreamTaskRequest{
		TaskId: start.GetTaskId(),
		Cursor: &pb.StreamTaskRequest_AfterEventId{
			AfterEventId: pendingStatus.GetNewestBufferedEventId(),
		},
		ClientSubscriberId: "concurrent-live",
	})
	if err != nil {
		t.Fatalf("StreamTask(live) error = %v", err)
	}

	statusDone := make(chan error, 1)
	go func() {
		statusCtx, cancel := context.WithTimeout(grpcE2EContext(context.Background()), 200*time.Millisecond)
		defer cancel()
		_, err := harness.client.GetTaskStatus(statusCtx, taskIDLocator(start.GetTaskId()))
		statusDone <- err
	}()

	pendingRequest := pendingStatus.GetActivePendingRequests()[0]
	respondDone := make(chan error, 1)
	go func() {
		respond, err := harness.client.RespondPendingRequest(grpcE2EContext(context.Background()), &pb.RespondPendingRequestRequest{
			TaskId:           start.GetTaskId(),
			PendingRequestId: pendingRequest.GetPendingRequestId(),
			ClientResponseId: "client-response-concurrent",
			Response: &pb.RespondPendingRequestRequest_Approval{
				Approval: &pb.ApprovalPendingResponse{DecisionId: "decision-accept"},
			},
		})
		if err != nil {
			respondDone <- err
			return
		}
		if !respond.GetAccepted() || respond.GetResolvedEventId() == 0 {
			respondDone <- fmt.Errorf("RespondPendingRequest() response = %#v", respond)
			return
		}
		respondDone <- nil
	}()

	if err := waitAsyncError("GetTaskStatus while pending response runs", statusDone); err != nil {
		t.Fatal(err)
	}
	if err := waitAsyncError("RespondPendingRequest", respondDone); err != nil {
		t.Fatal(err)
	}
	pendingResolved := recvStreamEvent(t, live)
	if pendingResolved.GetPendingRequestResolved() == nil {
		t.Fatalf("live event after pending response = %#v, want pending resolution", pendingResolved)
	}

	interruptDone := make(chan error, 1)
	go func() {
		interrupt, err := harness.client.InterruptTask(grpcE2EContext(context.Background()), &pb.InterruptTaskRequest{
			Locator: &pb.InterruptTaskRequest_TaskId{
				TaskId: start.GetTaskId(),
			},
			ClientRequestId: "interrupt-concurrent",
		})
		if err != nil {
			interruptDone <- err
			return
		}
		if !interrupt.GetInterruptSent() || interrupt.GetTaskId() != start.GetTaskId() {
			interruptDone <- fmt.Errorf("InterruptTask() response = %#v", interrupt)
			return
		}
		interruptDone <- nil
	}()
	appServer.RequireOutboundRequest(t, 5, testappserver.MethodTurnInterrupt)

	interruptStatusDone := make(chan error, 1)
	go func() {
		statusCtx, cancel := context.WithTimeout(grpcE2EContext(context.Background()), 200*time.Millisecond)
		defer cancel()
		status, err := harness.client.GetTaskStatus(statusCtx, taskIDLocator(start.GetTaskId()))
		if err != nil {
			interruptStatusDone <- err
			return
		}
		if status.GetState() != pb.TaskState_TASK_STATE_INTERRUPTING {
			interruptStatusDone <- fmt.Errorf("GetTaskStatus() during interrupt = %#v", status)
			return
		}
		interruptStatusDone <- nil
	}()
	if err := waitAsyncError("GetTaskStatus while interrupt runs", interruptStatusDone); err != nil {
		t.Fatal(err)
	}
	interrupting := recvStreamEvent(t, live)
	if interrupting.GetLifecycle() == nil || interrupting.GetLifecycle().GetState() != pb.TaskState_TASK_STATE_INTERRUPTING {
		t.Fatalf("live event during interrupt = %#v, want interrupting lifecycle", interrupting)
	}
	interruptGate.release()
	if err := waitAsyncError("InterruptTask", interruptDone); err != nil {
		t.Fatal(err)
	}
	waitGRPCStatusState(t, harness.client, taskIDLocator(start.GetTaskId()), pb.TaskState_TASK_STATE_INTERRUPTED)
	appServer.RequireDone(t)
}

type grpcScriptGate struct {
	name   string
	once   sync.Once
	signal chan struct{}
}

func newGRPCScriptGate(t *testing.T, name string) *grpcScriptGate {
	t.Helper()
	return &grpcScriptGate{
		name:   name,
		signal: make(chan struct{}),
	}
}

func (g *grpcScriptGate) step() testappserver.Step {
	return testappserver.WaitForSignal(g.name, g.signal)
}

func (g *grpcScriptGate) release() {
	g.once.Do(func() {
		close(g.signal)
	})
}

type asyncGRPCStart struct {
	response *pb.StartTaskResponse
	err      error
	done     chan struct{}
}

func startGRPCAsync(client pb.CodexControlClient, request *pb.StartTaskRequest) *asyncGRPCStart {
	return startGRPCAsyncContext(client, grpcE2EContext(context.Background()), request)
}

func startGRPCAsyncContext(client pb.CodexControlClient, ctx context.Context, request *pb.StartTaskRequest) *asyncGRPCStart {
	result := &asyncGRPCStart{done: make(chan struct{})}
	go func() {
		result.response, result.err = client.StartTask(ctx, request)
		close(result.done)
	}()
	return result
}

func (s *asyncGRPCStart) wait(t *testing.T) (*pb.StartTaskResponse, error) {
	t.Helper()
	select {
	case <-s.done:
		return s.response, s.err
	case <-time.After(2 * time.Second):
		t.Fatal("StartTask() did not finish")
		return nil, nil
	}
}

func grpcServingE2EGroup(t *testing.T, sessionGroupID string, workspaceID string) config.SessionGroup {
	t.Helper()
	group := testSessionGroup(t, sessionGroupID, workspaceID)
	group.GRPCLimits = config.GRPCLimits{
		InboundMessageBytes:  4 * domain.MiB,
		OutboundMessageBytes: 4 * domain.MiB,
	}
	return group
}

func startTaskRequest(group config.SessionGroup, clientMessageID string, prompt string) *pb.StartTaskRequest {
	return &pb.StartTaskRequest{
		SessionGroupId:  group.SessionGroupID,
		WorkspaceId:     group.WorkspaceID,
		Prompt:          prompt,
		ClientMessageId: clientMessageID,
	}
}

func waitGRPCPendingCountByClient(
	t *testing.T,
	client pb.CodexControlClient,
	sessionGroupID string,
	clientMessageID string,
	count int,
) *pb.GetTaskStatusResponse {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last *pb.GetTaskStatusResponse
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(grpcE2EContext(context.Background()), 200*time.Millisecond)
		status, err := client.GetTaskStatus(ctx, clientMessageLocator(sessionGroupID, clientMessageID))
		cancel()
		if err == nil {
			last = status
			if status.GetTaskId() != "" && len(status.GetActivePendingRequests()) == count {
				return status
			}
		} else {
			lastErr = err
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("GetTaskStatus() by client did not reach pending count %d; last status=%#v err=%v", count, last, lastErr)
	return nil
}

func openGRPCStream(
	t *testing.T,
	client pb.CodexControlClient,
	request *pb.StreamTaskRequest,
) pb.CodexControl_StreamTaskClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(grpcE2EContext(context.Background()), time.Second)
	t.Cleanup(cancel)
	stream, err := client.StreamTask(ctx, request)
	if err != nil {
		t.Fatalf("StreamTask() error = %v", err)
	}
	return stream
}

func recvStreamNotice(t *testing.T, stream pb.CodexControl_StreamTaskClient) *pb.ReplayNotice {
	t.Helper()
	message, err := stream.Recv()
	if err != nil {
		t.Fatalf("StreamTask().Recv() error = %v", err)
	}
	if message.GetEvent() != nil || message.GetReplayNotice() == nil {
		t.Fatalf("stream message = %#v, want replay notice only", message)
	}
	return message.GetReplayNotice()
}

func assertReplayNotice(
	t *testing.T,
	notice *pb.ReplayNotice,
	code pb.ReplayNoticeCode,
	status *pb.GetTaskStatusResponse,
) {
	t.Helper()
	if notice.GetCode() != code ||
		notice.GetOldestBufferedEventId() != status.GetOldestBufferedEventId() ||
		notice.GetNewestBufferedEventId() != status.GetNewestBufferedEventId() ||
		notice.GetFromStartAvailable() ||
		notice.GetStartEvictedBeforeEventId() != status.GetStartEvictedBeforeEventId() {
		t.Fatalf("replay notice = %#v, code = %s, status = %#v", notice, code, status)
	}
}

func assertGRPCCode(t *testing.T, err error, code codes.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want code %s", code)
	}
	if got := status.Code(err); got != code {
		t.Fatalf("status code = %s, want %s (err=%v)", got, code, err)
	}
}

func waitAsyncError(name string, done <-chan error) error {
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	case <-time.After(time.Second):
		return fmt.Errorf("%s did not finish", name)
	}
}
