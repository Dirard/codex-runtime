package grpcapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"github.com/Dirard/codex-runtime/internal/config"
	"github.com/Dirard/codex-runtime/internal/domain"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	testBearerToken         = "stage3-fixture-token"
	testMaxRecvMessageBytes = 4 * domain.MiB
	testMaxSendMessageBytes = 4 * domain.MiB
)

func TestUnaryAuthRejectsMissingWrongMalformedDuplicateAndNonBearerMetadata(t *testing.T) {
	tests := []struct {
		name     string
		metadata metadata.MD
	}{
		{name: "missing"},
		{name: "empty", metadata: metadata.Pairs(authorizationMetadataKey, "")},
		{name: "whitespace only", metadata: metadata.Pairs(authorizationMetadataKey, "   ")},
		{name: "malformed bearer", metadata: metadata.Pairs(authorizationMetadataKey, "Bearer")},
		{name: "non bearer", metadata: metadata.Pairs(authorizationMetadataKey, "Basic "+testBearerToken)},
		{name: "wrong token", metadata: metadata.Pairs(authorizationMetadataKey, bearerPrefix+"wrong-token")},
		{name: "duplicate", metadata: metadata.Pairs(authorizationMetadataKey, bearerPrefix+testBearerToken, authorizationMetadataKey, bearerPrefix+testBearerToken)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskService := &fakeTaskService{}
			harness := startGRPCTestHarness(t, testBearerToken, testControlServices(taskService, &fakePendingService{}))
			ctx := context.Background()
			if len(tt.metadata) > 0 {
				ctx = metadata.NewOutgoingContext(ctx, tt.metadata)
			}

			_, err := harness.client.StartTask(ctx, validStartTaskRequest())

			assertStatusCode(t, err, codes.Unauthenticated)
			assertStatusDoesNotContain(t, err, testBearerToken)
			if got := taskService.startCallCount(); got != 0 {
				t.Fatalf("StartTask reached service %d times, want 0", got)
			}
		})
	}
}

func TestAuthenticatedUnaryReachesTaskService(t *testing.T) {
	taskService := &fakeTaskService{}
	harness := startGRPCTestHarness(t, testBearerToken, testControlServices(taskService, &fakePendingService{}))

	response, err := harness.client.StartTask(authenticatedContext(context.Background(), testBearerToken), validStartTaskRequest())

	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	if response.GetTaskId() != "task-1" {
		t.Fatalf("StartTask() task_id = %q, want task-1", response.GetTaskId())
	}
	command := taskService.startCommand()
	want := domain.StartTaskCommand{
		SessionGroupID:  "sg-1",
		WorkspaceID:     "ws-1",
		Prompt:          "hello",
		ClientMessageID: "client-message-1",
		ContextBlocks: []domain.ContextBlock{
			{
				Kind:        domain.ContextBlockKindApplication,
				SourceLabel: "ticket",
				SourceURI:   "https://example.com/source",
				MimeType:    "text/plain",
				Content:     "context",
			},
		},
		UICorrelationMetadata: map[string]string{},
	}
	if !reflect.DeepEqual(command, want) {
		t.Fatalf("StartTask command = %#v, want %#v", command, want)
	}
}

func TestAuthenticatedUnarySanitizesAuthorizationMetadataBeforeService(t *testing.T) {
	taskService := &fakeTaskService{}
	harness := startGRPCTestHarness(t, testBearerToken, testControlServices(taskService, &fakePendingService{}))
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		authorizationMetadataKey, bearerPrefix+testBearerToken,
		"x-request-id", "request-1",
	))

	_, err := harness.client.StartTask(ctx, minimalStartTaskRequest())

	if err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	assertMetadataKeyAbsent(t, taskService.startMetadata(), authorizationMetadataKey)
	assertMetadataValue(t, taskService.startMetadata(), "x-request-id", "request-1")
}

func TestAuthenticatedStreamReachesTaskServiceAndCleansUpOnClientCancellation(t *testing.T) {
	taskStream := newBlockingTaskStream(streamEvent(domain.TaskEvent{
		EventID:        1,
		TaskID:         "task-1",
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		Payload: domain.TaskLifecycleEvent{
			LifecycleEvent: domain.TaskLifecycleEventTaskStarted,
			State:          domain.TaskStateRunning,
		},
	}))
	taskService := &fakeTaskService{stream: taskStream}
	harness := startGRPCTestHarness(t, testBearerToken, testControlServices(taskService, &fakePendingService{}))
	ctx, cancel := context.WithCancel(authenticatedContext(context.Background(), testBearerToken))
	clientStream, err := harness.client.StreamTask(ctx, &pb.StreamTaskRequest{
		TaskId: "task-1",
		Cursor: &pb.StreamTaskRequest_FromStart{
			FromStart: &pb.FromStartCursor{},
		},
		ClientSubscriberId: "subscriber-1",
	})
	if err != nil {
		t.Fatalf("StreamTask() error = %v", err)
	}

	message, err := clientStream.Recv()
	if err != nil {
		t.Fatalf("StreamTask().Recv() error = %v", err)
	}
	if message.GetEvent().GetEventId() != 1 {
		t.Fatalf("stream event_id = %d, want 1", message.GetEvent().GetEventId())
	}
	command := taskService.streamCommand()
	want := domain.StreamTaskCommand{
		TaskID:             "task-1",
		CursorKind:         domain.StreamCursorFromStart,
		ClientSubscriberID: "subscriber-1",
	}
	if !reflect.DeepEqual(command, want) {
		t.Fatalf("StreamTask command = %#v, want %#v", command, want)
	}

	cancel()
	if _, err := clientStream.Recv(); err == nil {
		t.Fatal("StreamTask().Recv() after cancel = nil, want cancellation error")
	}
	waitForChannelClosed(t, taskStream.contextCanceled, "stream context cancellation")
	waitForChannelClosed(t, taskStream.closed, "stream close")
}

func TestAuthenticatedStreamSanitizesAuthorizationMetadataBeforeService(t *testing.T) {
	taskService := &fakeTaskService{}
	harness := startGRPCTestHarness(t, testBearerToken, testControlServices(taskService, &fakePendingService{}))
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		authorizationMetadataKey, bearerPrefix+testBearerToken,
		"x-request-id", "stream-request-1",
	))

	clientStream, err := harness.client.StreamTask(ctx, &pb.StreamTaskRequest{
		TaskId: "task-1",
		Cursor: &pb.StreamTaskRequest_FromStart{
			FromStart: &pb.FromStartCursor{},
		},
	})
	if err == nil {
		_, err = clientStream.Recv()
	}

	if !errors.Is(err, io.EOF) {
		t.Fatalf("StreamTask().Recv() error = %v, want EOF", err)
	}
	assertMetadataKeyAbsent(t, taskService.streamMetadata(), authorizationMetadataKey)
	assertMetadataValue(t, taskService.streamMetadata(), "x-request-id", "stream-request-1")
}

func TestNewServerFromConfigUsesMaximumSessionGroupTransportLimits(t *testing.T) {
	validated := validatedConfigWithGRPCLimits(t, []grpcLimitSpec{
		{sessionGroupID: "sg-1", workspaceID: "ws-1", inbound: 2 * domain.MiB, outbound: 3 * domain.MiB},
		{sessionGroupID: "sg-2", workspaceID: "ws-2", inbound: 6 * domain.MiB, outbound: 5 * domain.MiB},
	})
	server, err := NewServerFromConfig(validated, &fakeTaskService{}, &fakePendingService{})
	if err != nil {
		t.Fatalf("NewServerFromConfig() error = %v", err)
	}
	defer server.Stop()

	if server.maxRecvMessageBytes != 6*domain.MiB {
		t.Fatalf("server max recv = %d, want %d", server.maxRecvMessageBytes, 6*domain.MiB)
	}
	if server.maxSendMessageBytes != 5*domain.MiB {
		t.Fatalf("server max send = %d, want %d", server.maxSendMessageBytes, 5*domain.MiB)
	}
}

func TestConfigDerivedServerRejectsStartTaskAboveLowSessionInboundCapBelowProcessMax(t *testing.T) {
	req := minimalStartTaskRequest()
	req.SessionGroupId = "sg-low"
	req.ClientMessageId = "client-low"
	req.Prompt = strings.Repeat("x", 512)
	serializedSize := proto.Size(req)
	lowInboundCap := serializedSize - 1
	highInboundCap := serializedSize + 1024
	taskService := &fakeTaskService{}
	validated := validatedConfigWithGRPCLimits(t, []grpcLimitSpec{
		{sessionGroupID: "sg-low", workspaceID: "ws-low", inbound: int64(lowInboundCap), outbound: 2 * domain.MiB},
		{sessionGroupID: "sg-high", workspaceID: "ws-high", inbound: int64(highInboundCap), outbound: 3 * domain.MiB},
	})
	server, err := NewServerFromConfig(validated, taskService, &fakePendingService{})
	if err != nil {
		t.Fatalf("NewServerFromConfig() error = %v", err)
	}
	harness := startGRPCTestHarnessFromServer(t, server)

	_, err = harness.client.StartTask(authenticatedContext(context.Background(), testBearerToken), req)

	assertStatusCode(t, err, codes.ResourceExhausted)
	assertStatusDoesNotContain(t, err, testBearerToken)
	if server.maxRecvMessageBytes != highInboundCap {
		t.Fatalf("server max recv = %d, want %d", server.maxRecvMessageBytes, highInboundCap)
	}
	if got := taskService.startCallCount(); got != 0 {
		t.Fatalf("low-session oversized StartTask reached service %d times, want 0", got)
	}
}

func TestPerSessionInboundLimitAllowsStartTaskUnderLowSessionCap(t *testing.T) {
	req := minimalStartTaskRequest()
	taskService := &fakeTaskService{}
	services := ControlServices{
		SessionGroups: testResolver{
			"sg-1": testSessionGroupMetadataWithLimits("sg-1", "ws-1", proto.Size(req), testMaxSendMessageBytes),
		},
		Tasks:   taskService,
		Pending: &fakePendingService{},
	}
	harness := startGRPCTestHarness(t, testBearerToken, services)

	_, err := harness.client.StartTask(authenticatedContext(context.Background(), testBearerToken), req)

	if err != nil {
		t.Fatalf("StartTask() under session inbound cap error = %v", err)
	}
	if got := taskService.startCallCount(); got != 1 {
		t.Fatalf("StartTask reached service %d times, want 1", got)
	}
}

func TestRespondPendingRequestChecksResolvedSessionInboundCapBeforeService(t *testing.T) {
	req := minimalRespondPendingRequest()
	serializedSize := proto.Size(req)
	lowInboundCap := serializedSize - 1
	highInboundCap := serializedSize + 1024
	pendingService := &fakePendingService{
		resolvedMetadata: testSessionGroupMetadataWithLimits("sg-low", "ws-low", lowInboundCap, testMaxSendMessageBytes),
	}
	services := ControlServices{
		SessionGroups: testResolver{
			"sg-low":  testSessionGroupMetadataWithLimits("sg-low", "ws-low", lowInboundCap, testMaxSendMessageBytes),
			"sg-high": testSessionGroupMetadataWithLimits("sg-high", "ws-high", highInboundCap, testMaxSendMessageBytes),
		},
		Tasks:   &fakeTaskService{},
		Pending: pendingService,
	}
	harness := startGRPCTestHarnessWithOptions(t, ServerOptions{
		ListenAddress:       "127.0.0.1:0",
		AuthToken:           testBearerToken,
		MaxRecvMessageBytes: highInboundCap,
		MaxSendMessageBytes: testMaxSendMessageBytes,
		Services:            services,
	})

	_, err := harness.client.RespondPendingRequest(authenticatedContext(context.Background(), testBearerToken), req)

	assertStatusCode(t, err, codes.ResourceExhausted)
	assertStatusDoesNotContain(t, err, testBearerToken)
	if got := pendingService.respondCallCount(); got != 0 {
		t.Fatalf("RespondPendingRequest reached service %d times, want 0", got)
	}
}

func TestNewServerRejectsMissingTransportLimits(t *testing.T) {
	_, err := NewServer(ServerOptions{
		ListenAddress: "127.0.0.1:0",
		AuthToken:     testBearerToken,
		Services:      testControlServices(&fakeTaskService{}, &fakePendingService{}),
	})

	if err == nil {
		t.Fatal("NewServer() accepted missing transport limits")
	}
}

func TestNewServerRejectsTransportLimitsAboveHardCap(t *testing.T) {
	_, err := NewServer(ServerOptions{
		ListenAddress:       "127.0.0.1:0",
		AuthToken:           testBearerToken,
		MaxRecvMessageBytes: config.MaxGRPCMessageBytes + 1,
		MaxSendMessageBytes: testMaxSendMessageBytes,
		Services:            testControlServices(&fakeTaskService{}, &fakePendingService{}),
	})

	if err == nil {
		t.Fatal("NewServer() accepted max receive limit above hard cap")
	}
}

func TestProcessRecvLimitRejectsOversizedInboundRequestWithoutTokenLeak(t *testing.T) {
	taskService := &fakeTaskService{}
	harness := startGRPCTestHarnessWithOptions(t, ServerOptions{
		ListenAddress:       "127.0.0.1:0",
		AuthToken:           testBearerToken,
		MaxRecvMessageBytes: 256,
		MaxSendMessageBytes: testMaxSendMessageBytes,
		Services:            testControlServices(taskService, &fakePendingService{}),
	})
	req := minimalStartTaskRequest()
	req.Prompt = strings.Repeat("x", 1024)

	_, err := harness.client.StartTask(authenticatedContext(context.Background(), testBearerToken), req)

	assertStatusCode(t, err, codes.ResourceExhausted)
	assertStatusReason(t, err, domain.ReasonResourceExhausted)
	assertStatusDoesNotContain(t, err, testBearerToken)
	if got := taskService.startCallCount(); got != 0 {
		t.Fatalf("oversized StartTask reached service %d times, want 0", got)
	}
}

func TestTransportRecvLimitAllowsRequestUnderConfiguredCap(t *testing.T) {
	taskService := &fakeTaskService{}
	harness := startGRPCTestHarnessWithOptions(t, ServerOptions{
		ListenAddress:       "127.0.0.1:0",
		AuthToken:           testBearerToken,
		MaxRecvMessageBytes: 256,
		MaxSendMessageBytes: testMaxSendMessageBytes,
		Services:            testControlServices(taskService, &fakePendingService{}),
	})

	_, err := harness.client.StartTask(authenticatedContext(context.Background(), testBearerToken), minimalStartTaskRequest())

	if err != nil {
		t.Fatalf("StartTask() under recv cap error = %v", err)
	}
	if got := taskService.startCallCount(); got != 1 {
		t.Fatalf("StartTask reached service %d times, want 1", got)
	}
}

func TestProcessSendLimitRejectsOversizedOutboundResponseWithoutTokenLeak(t *testing.T) {
	taskService := &fakeTaskService{}
	harness := startGRPCTestHarnessWithOptions(t, ServerOptions{
		ListenAddress:       "127.0.0.1:0",
		AuthToken:           testBearerToken,
		MaxRecvMessageBytes: testMaxRecvMessageBytes,
		MaxSendMessageBytes: 16,
		Services:            testControlServices(taskService, &fakePendingService{}),
	})

	_, err := harness.client.StartTask(authenticatedContext(context.Background(), testBearerToken), minimalStartTaskRequest())

	assertStatusCode(t, err, codes.ResourceExhausted)
	assertStatusReason(t, err, domain.ReasonResourceExhausted)
	assertStatusDoesNotContain(t, err, testBearerToken)
	if got := taskService.startCallCount(); got != 1 {
		t.Fatalf("StartTask reached service %d times, want 1", got)
	}
}

func TestProcessSendLimitRejectsOversizedStreamMessageWithGatewayDetails(t *testing.T) {
	taskStream := newBlockingTaskStream(streamEvent(domain.TaskEvent{
		EventID:        1,
		TaskID:         "task-1",
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		Payload: domain.TaskLifecycleEvent{
			LifecycleEvent: domain.TaskLifecycleEventTaskStarted,
			State:          domain.TaskStateRunning,
		},
	}))
	taskService := &fakeTaskService{stream: taskStream}
	harness := startGRPCTestHarnessWithOptions(t, ServerOptions{
		ListenAddress:       "127.0.0.1:0",
		AuthToken:           testBearerToken,
		MaxRecvMessageBytes: testMaxRecvMessageBytes,
		MaxSendMessageBytes: 16,
		Services:            testControlServices(taskService, &fakePendingService{}),
	})

	clientStream, err := harness.client.StreamTask(authenticatedContext(context.Background(), testBearerToken), &pb.StreamTaskRequest{
		TaskId: "task-1",
		Cursor: &pb.StreamTaskRequest_FromStart{
			FromStart: &pb.FromStartCursor{},
		},
	})
	if err == nil {
		_, err = clientStream.Recv()
	}

	assertStatusCode(t, err, codes.ResourceExhausted)
	assertStatusReason(t, err, domain.ReasonResourceExhausted)
	assertStatusDoesNotContain(t, err, testBearerToken)
	waitForChannelClosed(t, taskStream.closed, "stream close")
}

func TestPerSessionOutboundLimitRejectsOversizedStartTaskResponseWithoutTokenLeak(t *testing.T) {
	taskService := &fakeTaskService{}
	services := ControlServices{
		SessionGroups: testResolver{
			"sg-1": testSessionGroupMetadataWithLimits("sg-1", "ws-1", testMaxRecvMessageBytes, 8),
		},
		Tasks:   taskService,
		Pending: &fakePendingService{},
	}
	harness := startGRPCTestHarness(t, testBearerToken, services)

	_, err := harness.client.StartTask(authenticatedContext(context.Background(), testBearerToken), minimalStartTaskRequest())

	assertStatusCode(t, err, codes.ResourceExhausted)
	assertStatusDoesNotContain(t, err, testBearerToken)
	if got := taskService.startCallCount(); got != 1 {
		t.Fatalf("StartTask reached service %d times, want 1", got)
	}
}

func TestStartTaskOutboundRejectsServiceSessionMismatchAgainstTrustedRequestSession(t *testing.T) {
	response := domain.StartTaskResponse{
		TaskID:         "task-1",
		ThreadID:       "thread-1",
		TurnID:         "turn-1",
		SessionGroupID: "sg-high",
		State:          domain.TaskStateRunning,
		LastEventID:    1,
	}
	taskService := &fakeTaskService{startResponse: &response}
	services := ControlServices{
		SessionGroups: testResolver{
			"sg-low":  testSessionGroupMetadataWithLimits("sg-low", "ws-low", testMaxRecvMessageBytes, 8),
			"sg-high": testSessionGroupMetadataWithLimits("sg-high", "ws-high", testMaxRecvMessageBytes, testMaxSendMessageBytes),
		},
		Tasks:   taskService,
		Pending: &fakePendingService{},
	}
	harness := startGRPCTestHarness(t, testBearerToken, services)
	req := minimalStartTaskRequest()
	req.SessionGroupId = "sg-low"

	_, err := harness.client.StartTask(authenticatedContext(context.Background(), testBearerToken), req)

	assertStatusCode(t, err, codes.Internal)
	assertStatusReason(t, err, domain.ReasonInternalGatewayError)
	assertStatusDoesNotContain(t, err, testBearerToken)
	assertStatusDoesNotContain(t, err, "sg-high")
	if got := taskService.startCallCount(); got != 1 {
		t.Fatalf("StartTask reached service %d times, want 1", got)
	}
}

func TestRespondPendingRequestOutboundRejectsServiceSessionMismatchAgainstResolvedSession(t *testing.T) {
	response := domain.RespondPendingRequestResponse{
		TaskID:           "task-1",
		SessionGroupID:   "sg-high",
		PendingRequestID: "pending-1",
		ClientResponseID: "client-response-1",
		Accepted:         true,
	}
	pendingService := &fakePendingService{
		resolvedMetadata: testSessionGroupMetadataWithLimits("sg-low", "ws-low", testMaxRecvMessageBytes, 8),
		respondResponse:  &response,
	}
	services := ControlServices{
		SessionGroups: testResolver{
			"sg-low":  testSessionGroupMetadataWithLimits("sg-low", "ws-low", testMaxRecvMessageBytes, 8),
			"sg-high": testSessionGroupMetadataWithLimits("sg-high", "ws-high", testMaxRecvMessageBytes, testMaxSendMessageBytes),
		},
		Tasks:   &fakeTaskService{},
		Pending: pendingService,
	}
	harness := startGRPCTestHarness(t, testBearerToken, services)

	_, err := harness.client.RespondPendingRequest(authenticatedContext(context.Background(), testBearerToken), minimalRespondPendingRequest())

	assertStatusCode(t, err, codes.Internal)
	assertStatusReason(t, err, domain.ReasonInternalGatewayError)
	assertStatusDoesNotContain(t, err, testBearerToken)
	assertStatusDoesNotContain(t, err, "sg-high")
	if got := pendingService.respondCallCount(); got != 1 {
		t.Fatalf("RespondPendingRequest reached service %d times, want 1", got)
	}
}

func TestGetTaskStatusKnownLocatorOutboundRejectsServiceSessionMismatchAgainstTrustedLocator(t *testing.T) {
	response := domain.GetTaskStatusResponse{
		TaskID:         "task-1",
		State:          domain.TaskStateRunning,
		SessionGroupID: "sg-high",
		WorkspaceID:    "ws-high",
	}
	taskService := &fakeTaskService{statusResponse: &response}
	services := ControlServices{
		SessionGroups: testResolver{
			"sg-low":  testSessionGroupMetadataWithLimits("sg-low", "ws-low", testMaxRecvMessageBytes, 8),
			"sg-high": testSessionGroupMetadataWithLimits("sg-high", "ws-high", testMaxRecvMessageBytes, testMaxSendMessageBytes),
		},
		Tasks:   taskService,
		Pending: &fakePendingService{},
	}
	harness := startGRPCTestHarness(t, testBearerToken, services)

	_, err := harness.client.GetTaskStatus(authenticatedContext(context.Background(), testBearerToken), &pb.GetTaskStatusRequest{
		Locator: &pb.GetTaskStatusRequest_ClientMessageLocator{
			ClientMessageLocator: &pb.ClientMessageTaskLocator{
				SessionGroupId:  "sg-low",
				ClientMessageId: "client-message-1",
			},
		},
	})

	assertStatusCode(t, err, codes.Internal)
	assertStatusReason(t, err, domain.ReasonInternalGatewayError)
	assertStatusDoesNotContain(t, err, testBearerToken)
	assertStatusDoesNotContain(t, err, "sg-high")
}

func TestGetTaskStatusTaskIDOnlyStillUsesResponseSessionForOutboundValidation(t *testing.T) {
	response := domain.GetTaskStatusResponse{
		TaskID:         "task-1",
		State:          domain.TaskStateRunning,
		SessionGroupID: "sg-high",
		WorkspaceID:    "ws-high",
	}
	taskService := &fakeTaskService{statusResponse: &response}
	services := ControlServices{
		SessionGroups: testResolver{
			"sg-high": testSessionGroupMetadataWithLimits("sg-high", "ws-high", testMaxRecvMessageBytes, testMaxSendMessageBytes),
		},
		Tasks:   taskService,
		Pending: &fakePendingService{},
	}
	harness := startGRPCTestHarness(t, testBearerToken, services)

	got, err := harness.client.GetTaskStatus(authenticatedContext(context.Background(), testBearerToken), &pb.GetTaskStatusRequest{
		Locator: &pb.GetTaskStatusRequest_TaskId{TaskId: "task-1"},
	})

	if err != nil {
		t.Fatalf("GetTaskStatus() task-id-only error = %v", err)
	}
	if got.GetSessionGroupId() != "sg-high" {
		t.Fatalf("GetTaskStatus() session_group_id = %q, want sg-high", got.GetSessionGroupId())
	}
}

func TestPerSessionOutboundLimitRejectsOversizedStreamEventWithoutTokenLeak(t *testing.T) {
	taskStream := newBlockingTaskStream(streamEvent(domain.TaskEvent{
		EventID:        1,
		TaskID:         "task-1",
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
		Payload: domain.TaskLifecycleEvent{
			LifecycleEvent: domain.TaskLifecycleEventTaskStarted,
			State:          domain.TaskStateRunning,
		},
	}))
	taskService := &fakeTaskService{stream: taskStream}
	services := ControlServices{
		SessionGroups: testResolver{
			"sg-1": testSessionGroupMetadataWithLimits("sg-1", "ws-1", testMaxRecvMessageBytes, 8),
		},
		Tasks:   taskService,
		Pending: &fakePendingService{},
	}
	harness := startGRPCTestHarness(t, testBearerToken, services)

	clientStream, err := harness.client.StreamTask(authenticatedContext(context.Background(), testBearerToken), &pb.StreamTaskRequest{
		TaskId: "task-1",
		Cursor: &pb.StreamTaskRequest_FromStart{
			FromStart: &pb.FromStartCursor{},
		},
	})
	if err == nil {
		_, err = clientStream.Recv()
	}

	assertStatusCode(t, err, codes.ResourceExhausted)
	assertStatusDoesNotContain(t, err, testBearerToken)
	waitForChannelClosed(t, taskStream.closed, "stream close")
}

func TestPerSessionOutboundLimitRejectsOversizedReplayNoticeWithoutTokenLeak(t *testing.T) {
	taskStream := newBlockingTaskStream(StreamTaskMessage{
		SessionGroupID: "sg-1",
		ReplayNotice: &domain.ReplayNotice{
			Code:                      domain.ReplayNoticeCursorEvicted,
			Message:                   strings.Repeat("r", 64),
			OldestBufferedEventID:     1,
			NewestBufferedEventID:     2,
			FromStartAvailable:        true,
			StartEvictedBeforeEventID: 1,
		},
	})
	taskService := &fakeTaskService{stream: taskStream}
	services := ControlServices{
		SessionGroups: testResolver{
			"sg-1": testSessionGroupMetadataWithLimits("sg-1", "ws-1", testMaxRecvMessageBytes, 8),
		},
		Tasks:   taskService,
		Pending: &fakePendingService{},
	}
	harness := startGRPCTestHarness(t, testBearerToken, services)

	clientStream, err := harness.client.StreamTask(authenticatedContext(context.Background(), testBearerToken), &pb.StreamTaskRequest{
		TaskId: "task-1",
		Cursor: &pb.StreamTaskRequest_FromStart{
			FromStart: &pb.FromStartCursor{},
		},
	})
	if err == nil {
		_, err = clientStream.Recv()
	}

	assertStatusCode(t, err, codes.ResourceExhausted)
	assertStatusDoesNotContain(t, err, testBearerToken)
	waitForChannelClosed(t, taskStream.closed, "stream close")
}

func TestServerRegistersOnlyCodexControlService(t *testing.T) {
	server, err := NewServer(testServerOptions(testBearerToken, testControlServices(&fakeTaskService{}, &fakePendingService{})))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer server.Stop()

	names := server.ServiceNames()

	want := []string{"codex.control.v1.CodexControl"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("service names = %#v, want %#v", names, want)
	}
	for _, forbidden := range []string{"jsonrpc", "config", "file", "plugin", "mcp"} {
		for _, name := range names {
			if strings.Contains(strings.ToLower(name), forbidden) {
				t.Fatalf("service %q exposes forbidden surface %q", name, forbidden)
			}
		}
	}
}

func TestUnaryPanicRecoveryReturnsRedactedInternalStatus(t *testing.T) {
	panicText := "panic contains " + testBearerToken + " and raw detail"
	taskService := &fakeTaskService{panicText: panicText}
	harness := startGRPCTestHarness(t, testBearerToken, testControlServices(taskService, &fakePendingService{}))

	_, err := harness.client.StartTask(authenticatedContext(context.Background(), testBearerToken), validStartTaskRequest())

	assertStatusCode(t, err, codes.Internal)
	assertStatusReason(t, err, domain.ReasonInternalGatewayError)
	assertStatusDoesNotContain(t, err, testBearerToken)
	assertStatusDoesNotContain(t, err, "raw detail")
	assertStatusDoesNotContain(t, err, "panic contains")
}

func TestStreamPanicRecoveryReturnsRedactedInternalStatus(t *testing.T) {
	panicText := "stream panic contains " + testBearerToken + " and raw detail"
	taskService := &fakeTaskService{streamPanicText: panicText}
	harness := startGRPCTestHarness(t, testBearerToken, testControlServices(taskService, &fakePendingService{}))

	clientStream, err := harness.client.StreamTask(authenticatedContext(context.Background(), testBearerToken), &pb.StreamTaskRequest{
		TaskId: "task-1",
		Cursor: &pb.StreamTaskRequest_FromStart{
			FromStart: &pb.FromStartCursor{},
		},
	})
	if err == nil {
		_, err = clientStream.Recv()
	}

	assertStatusCode(t, err, codes.Internal)
	assertStatusReason(t, err, domain.ReasonInternalGatewayError)
	assertStatusDoesNotContain(t, err, testBearerToken)
	assertStatusDoesNotContain(t, err, "raw detail")
	assertStatusDoesNotContain(t, err, "stream panic contains")
}

func TestServiceSuppliedGatewayStatusWithUnsafeDetailsReturnsRedactedInternal(t *testing.T) {
	rawDetail := "raw service detail contains " + testBearerToken
	serviceStatus := status.New(codes.InvalidArgument, rawDetail)
	serviceStatusWithDetails, err := serviceStatus.WithDetails(&pb.GatewayErrorDetails{
		Reason:         string(domain.ReasonInvalidRequest),
		DisplayMessage: rawDetail,
		TaskId:         strings.Repeat("x", domain.MaxPublicIDBytes+1),
	})
	if err != nil {
		t.Fatalf("WithDetails() error = %v", err)
	}
	taskService := &fakeTaskService{startErr: serviceStatusWithDetails.Err()}
	harness := startGRPCTestHarness(t, testBearerToken, testControlServices(taskService, &fakePendingService{}))

	_, err = harness.client.StartTask(authenticatedContext(context.Background(), testBearerToken), validStartTaskRequest())

	assertStatusCode(t, err, codes.Internal)
	assertStatusReason(t, err, domain.ReasonInternalGatewayError)
	assertStatusDoesNotContain(t, err, testBearerToken)
	assertStatusDoesNotContain(t, err, "raw service detail")
}

func TestServiceSuppliedGatewayStatusWithValidSizedRawDetailsReturnsRedactedInternal(t *testing.T) {
	rawDetail := "valid-sized raw service detail contains " + testBearerToken
	serviceStatus := status.New(codes.InvalidArgument, rawDetail)
	serviceStatusWithDetails, err := serviceStatus.WithDetails(&pb.GatewayErrorDetails{
		Reason:         string(domain.ReasonInvalidRequest),
		DisplayMessage: rawDetail,
		TaskId:         "task-1",
	})
	if err != nil {
		t.Fatalf("WithDetails() error = %v", err)
	}
	taskService := &fakeTaskService{startErr: serviceStatusWithDetails.Err()}
	harness := startGRPCTestHarness(t, testBearerToken, testControlServices(taskService, &fakePendingService{}))

	_, err = harness.client.StartTask(authenticatedContext(context.Background(), testBearerToken), validStartTaskRequest())

	assertStatusCode(t, err, codes.Internal)
	assertStatusReason(t, err, domain.ReasonInternalGatewayError)
	assertStatusDoesNotContain(t, err, testBearerToken)
	assertStatusDoesNotContain(t, err, "raw service detail")
}

func startGRPCTestHarness(t *testing.T, token string, services ControlServices) *grpcTestHarness {
	t.Helper()
	return startGRPCTestHarnessWithOptions(t, testServerOptions(token, services))
}

func startGRPCTestHarnessWithOptions(t *testing.T, options ServerOptions) *grpcTestHarness {
	t.Helper()
	server, err := NewServer(options)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	return startGRPCTestHarnessFromServer(t, server)
}

func startGRPCTestHarnessFromServer(t *testing.T, server *Server) *grpcTestHarness {
	t.Helper()
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve()
	}()
	conn, err := grpc.NewClient(server.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
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
	return &grpcTestHarness{
		client: pb.NewCodexControlClient(conn),
	}
}

type grpcTestHarness struct {
	client pb.CodexControlClient
}

func testServerOptions(token string, services ControlServices) ServerOptions {
	return ServerOptions{
		ListenAddress:       "127.0.0.1:0",
		AuthToken:           token,
		MaxRecvMessageBytes: testMaxRecvMessageBytes,
		MaxSendMessageBytes: testMaxSendMessageBytes,
		Services:            services,
	}
}

func testSessionGroupMetadata(sessionGroupID string, workspaceID string) domain.SessionGroupMetadata {
	return testSessionGroupMetadataWithLimits(sessionGroupID, workspaceID, testMaxRecvMessageBytes, testMaxSendMessageBytes)
}

func testSessionGroupMetadataWithLimits(sessionGroupID string, workspaceID string, inbound int, outbound int) domain.SessionGroupMetadata {
	return domain.SessionGroupMetadata{
		SessionGroupID:           sessionGroupID,
		WorkspaceID:              workspaceID,
		GRPCInboundMessageBytes:  inbound,
		GRPCOutboundMessageBytes: outbound,
	}
}

func testControlServices(tasks TaskService, pending PendingService) ControlServices {
	return ControlServices{
		SessionGroups: testResolver{
			"sg-1": testSessionGroupMetadata("sg-1", "ws-1"),
		},
		Tasks:   tasks,
		Pending: pending,
	}
}

func minimalStartTaskRequest() *pb.StartTaskRequest {
	return &pb.StartTaskRequest{
		SessionGroupId:  "sg-1",
		Prompt:          "ok",
		ClientMessageId: "client-message-1",
	}
}

func minimalRespondPendingRequest() *pb.RespondPendingRequestRequest {
	return &pb.RespondPendingRequestRequest{
		TaskId:           "task-1",
		PendingRequestId: "pending-1",
		ClientResponseId: "client-response-1",
		Response: &pb.RespondPendingRequestRequest_Approval{
			Approval: &pb.ApprovalPendingResponse{DecisionId: "decline"},
		},
	}
}

type grpcLimitSpec struct {
	sessionGroupID string
	workspaceID    string
	inbound        int64
	outbound       int64
}

func validatedConfigWithGRPCLimits(t *testing.T, specs []grpcLimitSpec) *config.ValidatedConfig {
	t.Helper()
	t.Setenv("CODEX_CONTROL_GATEWAY_STAGE3_TEST_TOKEN", testBearerToken)
	tempDir := t.TempDir()
	codexBinary := filepath.Join(tempDir, codexExecutableName())
	if err := os.WriteFile(codexBinary, []byte("fixture executable"), 0o555); err != nil {
		t.Fatalf("WriteFile(codex binary) error = %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(codexBinary, 0o555); err != nil {
			t.Fatalf("Chmod(codex binary) error = %v", err)
		}
	}
	cwd := filepath.Join(tempDir, "cwd")
	if err := os.Mkdir(cwd, 0o755); err != nil {
		t.Fatalf("Mkdir(cwd) error = %v", err)
	}

	var groups strings.Builder
	for _, spec := range specs {
		codexHome := filepath.Join(tempDir, spec.sessionGroupID+"-codex-home")
		if err := os.Mkdir(codexHome, 0o755); err != nil {
			t.Fatalf("Mkdir(codex home) error = %v", err)
		}
		fmt.Fprintf(&groups, `[[session_groups]]
session_group_id = %s
workspace_id = %s
cwd = %s
codex_home = %s

[session_groups.runtime_policy]
approval_policy = "on-request"
approvals_reviewer = "user"
sandbox_mode = "workspace-write"

[session_groups.grpc_limits]
inbound_message_bytes = %d
outbound_message_bytes = %d
`, strconv.Quote(spec.sessionGroupID), strconv.Quote(spec.workspaceID), strconv.Quote(cwd), strconv.Quote(codexHome), spec.inbound, spec.outbound)
	}
	raw, err := config.ParseTOML([]byte(fmt.Sprintf(`codex_binary = %s
listen = "127.0.0.1:0"

[client_auth_token_source]
env = "CODEX_CONTROL_GATEWAY_STAGE3_TEST_TOKEN"

%s`, strconv.Quote(codexBinary), groups.String())))
	if err != nil {
		t.Fatalf("ParseTOML() error = %v", err)
	}
	validated, err := raw.Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	return validated
}

func codexExecutableName() string {
	if runtime.GOOS == "windows" {
		return "codex.exe"
	}
	return "codex"
}

func authenticatedContext(ctx context.Context, token string) context.Context {
	return metadata.NewOutgoingContext(ctx, metadata.Pairs(authorizationMetadataKey, bearerPrefix+token))
}

func incomingMetadataCopy(ctx context.Context) metadata.MD {
	metadataValues, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return metadata.MD{}
	}
	return metadataCopy(metadataValues)
}

func metadataCopy(metadataValues metadata.MD) metadata.MD {
	if metadataValues == nil {
		return metadata.MD{}
	}
	return metadataValues.Copy()
}

type fakeTaskService struct {
	mu                sync.Mutex
	startCalls        int
	recordedStart     domain.StartTaskCommand
	startMD           metadata.MD
	streamCalls       int
	recordedStream    domain.StreamTaskCommand
	streamMD          metadata.MD
	stream            TaskStream
	startErr          error
	startResponse     *domain.StartTaskResponse
	interruptResponse *domain.InterruptTaskResponse
	statusResponse    *domain.GetTaskStatusResponse
	panicText         string
	streamPanicText   string
}

func (s *fakeTaskService) StartTask(ctx context.Context, command domain.StartTaskCommand) (domain.StartTaskResponse, error) {
	if s.panicText != "" {
		panic(s.panicText)
	}
	s.mu.Lock()
	s.startCalls++
	s.recordedStart = command
	s.startMD = incomingMetadataCopy(ctx)
	s.mu.Unlock()
	if s.startErr != nil {
		return domain.StartTaskResponse{}, s.startErr
	}
	if s.startResponse != nil {
		return *s.startResponse, nil
	}
	return domain.StartTaskResponse{
		TaskID:         "task-1",
		ThreadID:       "thread-1",
		TurnID:         "turn-1",
		SessionGroupID: command.SessionGroupID,
		State:          domain.TaskStateRunning,
		LastEventID:    1,
	}, nil
}

func (s *fakeTaskService) StreamTask(ctx context.Context, command domain.StreamTaskCommand) (TaskStream, error) {
	if s.streamPanicText != "" {
		panic(s.streamPanicText)
	}
	s.mu.Lock()
	s.streamCalls++
	s.recordedStream = command
	s.streamMD = incomingMetadataCopy(ctx)
	stream := s.stream
	s.mu.Unlock()
	if stream == nil {
		return &eofTaskStream{}, nil
	}
	return stream, nil
}

func (s *fakeTaskService) InterruptTask(ctx context.Context, command domain.InterruptTaskCommand) (domain.InterruptTaskResponse, error) {
	if s.interruptResponse != nil {
		return *s.interruptResponse, nil
	}
	return domain.InterruptTaskResponse{
		TaskID:          "task-1",
		SessionGroupID:  "sg-1",
		State:           domain.TaskStateInterrupted,
		AlreadyTerminal: true,
	}, nil
}

func (s *fakeTaskService) GetTaskStatus(ctx context.Context, command domain.GetTaskStatusCommand) (domain.GetTaskStatusResponse, error) {
	if s.statusResponse != nil {
		return *s.statusResponse, nil
	}
	return domain.GetTaskStatusResponse{
		TaskID:         "task-1",
		State:          domain.TaskStateRunning,
		SessionGroupID: "sg-1",
		WorkspaceID:    "ws-1",
	}, nil
}

func (s *fakeTaskService) startCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startCalls
}

func (s *fakeTaskService) startCommand() domain.StartTaskCommand {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recordedStart
}

func (s *fakeTaskService) startMetadata() metadata.MD {
	s.mu.Lock()
	defer s.mu.Unlock()
	return metadataCopy(s.startMD)
}

func (s *fakeTaskService) streamCommand() domain.StreamTaskCommand {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recordedStream
}

func (s *fakeTaskService) streamMetadata() metadata.MD {
	s.mu.Lock()
	defer s.mu.Unlock()
	return metadataCopy(s.streamMD)
}

type fakePendingService struct {
	mu               sync.Mutex
	respondCalls     int
	resolvedMetadata domain.SessionGroupMetadata
	resolveErr       error
	respondErr       error
	respondResponse  *domain.RespondPendingRequestResponse
}

func (s *fakePendingService) ResolvePendingRequestSession(ctx context.Context, taskID string, pendingRequestID string) (domain.SessionGroupMetadata, error) {
	if s.resolveErr != nil {
		return domain.SessionGroupMetadata{}, s.resolveErr
	}
	if s.resolvedMetadata.SessionGroupID != "" {
		return s.resolvedMetadata, nil
	}
	return testSessionGroupMetadata("sg-1", "ws-1"), nil
}

func (s *fakePendingService) RespondPendingRequest(ctx context.Context, command domain.RespondPendingRequestCommand) (domain.RespondPendingRequestResponse, error) {
	s.mu.Lock()
	s.respondCalls++
	s.mu.Unlock()
	if s.respondErr != nil {
		return domain.RespondPendingRequestResponse{}, s.respondErr
	}
	if s.respondResponse != nil {
		return *s.respondResponse, nil
	}
	sessionGroupID := "sg-1"
	if s.resolvedMetadata.SessionGroupID != "" {
		sessionGroupID = s.resolvedMetadata.SessionGroupID
	}
	return domain.RespondPendingRequestResponse{
		TaskID:           command.TaskID,
		SessionGroupID:   sessionGroupID,
		PendingRequestID: command.PendingRequestID,
		ClientResponseID: command.ClientResponseID,
		Accepted:         true,
	}, nil
}

func (s *fakePendingService) respondCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.respondCalls
}

type eofTaskStream struct{}

func (s *eofTaskStream) Next(ctx context.Context) (StreamTaskMessage, error) {
	return StreamTaskMessage{}, io.EOF
}

func (s *eofTaskStream) Close() error {
	return nil
}

type blockingTaskStream struct {
	firstMessage    StreamTaskMessage
	once            sync.Once
	closeOnce       sync.Once
	contextOnce     sync.Once
	closed          chan struct{}
	contextCanceled chan struct{}
}

func newBlockingTaskStream(firstMessage StreamTaskMessage) *blockingTaskStream {
	return &blockingTaskStream{
		firstMessage:    firstMessage,
		closed:          make(chan struct{}),
		contextCanceled: make(chan struct{}),
	}
}

func (s *blockingTaskStream) Next(ctx context.Context) (StreamTaskMessage, error) {
	sent := false
	s.once.Do(func() {
		sent = true
	})
	if sent {
		return s.firstMessage, nil
	}
	<-ctx.Done()
	s.contextOnce.Do(func() {
		close(s.contextCanceled)
	})
	return StreamTaskMessage{}, ctx.Err()
}

func (s *blockingTaskStream) Close() error {
	s.closeOnce.Do(func() {
		close(s.closed)
	})
	return nil
}

func assertStatusCode(t *testing.T, err error, code codes.Code) {
	t.Helper()
	statusValue, ok := status.FromError(err)
	if !ok {
		t.Fatalf("status.FromError(%v) failed", err)
	}
	if statusValue.Code() != code {
		t.Fatalf("status code = %s, want %s", statusValue.Code(), code)
	}
}

func assertStatusReason(t *testing.T, err error, reason domain.GatewayErrorReason) {
	t.Helper()
	statusValue, ok := status.FromError(err)
	if !ok {
		t.Fatalf("status.FromError(%v) failed", err)
	}
	for _, detail := range statusValue.Details() {
		if gatewayDetails, ok := detail.(*pb.GatewayErrorDetails); ok {
			if gatewayDetails.GetReason() != string(reason) {
				t.Fatalf("GatewayErrorDetails.reason = %q, want %q", gatewayDetails.GetReason(), reason)
			}
			return
		}
	}
	t.Fatalf("status details = %#v, want GatewayErrorDetails", statusValue.Details())
}

func assertStatusDoesNotContain(t *testing.T, err error, text string) {
	t.Helper()
	statusValue, ok := status.FromError(err)
	if !ok {
		t.Fatalf("status.FromError(%v) failed", err)
	}
	if strings.Contains(statusValue.Message(), text) {
		t.Fatalf("status message %q contains forbidden text %q", statusValue.Message(), text)
	}
	for _, detail := range statusValue.Details() {
		if strings.Contains(detailText(detail), text) {
			t.Fatalf("status detail %#v contains forbidden text %q", detail, text)
		}
	}
}

func assertMetadataKeyAbsent(t *testing.T, metadataValues metadata.MD, key string) {
	t.Helper()
	if len(metadataValues.Get(key)) != 0 {
		t.Fatalf("metadata key %q value count = %d, want 0", key, len(metadataValues.Get(key)))
	}
}

func assertMetadataValue(t *testing.T, metadataValues metadata.MD, key string, want string) {
	t.Helper()
	values := metadataValues.Get(key)
	if len(values) != 1 || values[0] != want {
		t.Fatalf("metadata key %q values = %#v, want [%q]", key, values, want)
	}
}

func detailText(detail any) string {
	switch typed := detail.(type) {
	case *pb.GatewayErrorDetails:
		return strings.Join([]string{
			typed.GetReason(),
			typed.GetDisplayMessage(),
			typed.GetTaskId(),
			typed.GetSessionGroupId(),
			typed.GetClientMessageId(),
			typed.GetClientResponseId(),
			typed.GetPendingRequestId(),
			typed.GetThreadId(),
		}, " ")
	default:
		return ""
	}
}

func waitForChannelClosed(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func streamEvent(event domain.TaskEvent) StreamTaskMessage {
	return StreamTaskMessage{SessionGroupID: event.SessionGroupID, Event: &event}
}
