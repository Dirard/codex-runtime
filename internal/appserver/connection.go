package appserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/Dirard/codex-runtime/internal/config"
	"github.com/Dirard/codex-runtime/internal/domain"
	"github.com/Dirard/codex-runtime/internal/redact"
)

const (
	initializeTimeout             = 15 * time.Second
	defaultThreadCallTimeout      = 30 * time.Second
	defaultTurnStartTimeout       = 30 * time.Second
	defaultTurnInterruptTimeout   = 10 * time.Second
	defaultPendingResponseTimeout = 10 * time.Second
	unsupportedRequestTimeout     = 5 * time.Second
	authRefreshUnavailableCode    = -32001
	authRefreshUnavailableMessage = "auth_refresh_unavailable"
	authRefreshReasonUnauthorized = "unauthorized"
	unsupportedServerRequestCode  = -32002
)

// AuthRefreshFailure is the redacted metadata Stage 6 can use to fail a
// correlated task or emit a connection diagnostic.
type AuthRefreshFailure struct {
	SessionGroupID string
	TaskID         string
	Reason         domain.GatewayErrorReason
}

// UnsupportedServerRequest is safe task metadata for unsupported app-server
// requests. It never carries request params.
type UnsupportedServerRequest struct {
	SessionGroupID string
	TaskID         string
	ThreadID       string
	TurnID         string
	Method         string
}

// AuthRefreshFailureHook receives safe task/connection metadata after the
// app-server auth refresh request has failed closed.
type AuthRefreshFailureHook func(AuthRefreshFailure)

// UnsupportedServerRequestHook receives safe metadata after a server request
// has been rejected as unsupported.
type UnsupportedServerRequestHook func(UnsupportedServerRequest)

// ActiveTaskIDFunc returns the current active task id for auth-refresh failure
// handling when app-server does not carry task correlation metadata.
type ActiveTaskIDFunc func() string

type ThreadStartCall struct {
	TaskID  string
	Timeout time.Duration
}

type ThreadResumeCall struct {
	ThreadID string
	TaskID   string
	Timeout  time.Duration
}

type TurnStartCall struct {
	ThreadID            string
	ClientUserMessageID string
	Input               []UserInputText
	TaskID              string
	Timeout             time.Duration
}

type TurnInterruptCall struct {
	TurnID  string
	TaskID  string
	Timeout time.Duration
}

type Connection struct {
	dispatcher *Dispatcher
	session    config.SessionGroup
	policy     SchemaPolicy
	redactor   *redact.Redactor
	sensitive  *redact.Registry

	credentialProvider *config.CredentialProvider
	providerEnv        map[string]string
	requests           chan ServerRequest
	onClose            func()
	waitClose          func()

	hooksMu              sync.RWMutex
	activeTaskID         ActiveTaskIDFunc
	onAuthRefreshFailure AuthRefreshFailureHook
	onUnsupportedRequest UnsupportedServerRequestHook
}

type ConnectionOptions struct {
	CredentialProvider *config.CredentialProvider
	ProviderEnv        map[string]string
	SensitiveRegistry  *redact.Registry
	Redactor           *redact.Redactor
	SchemaPolicy       SchemaPolicy
	MaxLineBytes       int
	ForwardRequests    bool

	ActiveTaskID         ActiveTaskIDFunc
	OnAuthRefreshFailure AuthRefreshFailureHook
	OnUnsupportedRequest UnsupportedServerRequestHook
	// OnClose and WaitClose let process-backed connections bind dispatcher
	// closure to child process cancellation and exit.
	OnClose   func()
	WaitClose func()
}

func NewConnection(stdin io.WriteCloser, stdout io.ReadCloser, session config.SessionGroup, options ConnectionOptions) *Connection {
	registry := options.SensitiveRegistry
	if registry == nil {
		registry = redact.NewRegistry()
	}
	redactor := options.Redactor
	if redactor == nil {
		redactor = redact.New(redact.WithConnectionRegistry(registry))
	}
	var providerEnv map[string]string
	if options.CredentialProvider != nil {
		providerEnv = providerEnvMap(options.CredentialProvider.EnvSources, options.ProviderEnv)
	}
	connection := &Connection{
		dispatcher:           NewDispatcher(stdin, stdout, options.MaxLineBytes),
		session:              session,
		policy:               options.SchemaPolicy,
		redactor:             redactor,
		sensitive:            registry,
		credentialProvider:   options.CredentialProvider,
		providerEnv:          providerEnv,
		activeTaskID:         options.ActiveTaskID,
		onAuthRefreshFailure: options.OnAuthRefreshFailure,
		onUnsupportedRequest: options.OnUnsupportedRequest,
		onClose:              options.OnClose,
		waitClose:            options.WaitClose,
	}
	if options.ForwardRequests {
		connection.requests = make(chan ServerRequest, 32)
	}
	go connection.handleServerRequests()
	return connection
}

// SetAuthRefreshFailureHooks updates the service-owned task correlation hooks
// used when app-server auth refresh fails closed.
func (c *Connection) SetAuthRefreshFailureHooks(activeTaskID ActiveTaskIDFunc, hook AuthRefreshFailureHook) {
	if c == nil {
		return
	}
	c.hooksMu.Lock()
	defer c.hooksMu.Unlock()
	c.activeTaskID = activeTaskID
	c.onAuthRefreshFailure = hook
}

// SetUnsupportedServerRequestHook updates the service-owned hook used when
// app-server sends a request the gateway cannot handle.
func (c *Connection) SetUnsupportedServerRequestHook(hook UnsupportedServerRequestHook) {
	if c == nil {
		return
	}
	c.hooksMu.Lock()
	defer c.hooksMu.Unlock()
	c.onUnsupportedRequest = hook
}

func (c *Connection) StartThread(ctx context.Context, call ThreadStartCall) (json.RawMessage, error) {
	params, err := NewThreadStartParams(c.session)
	if err != nil {
		return nil, err
	}
	if params.Permissions != "" && !c.policy.CanStartWithPermissionsProfile() {
		return nil, schemaUnverifiedError(c.session.SessionGroupID, call.TaskID, "")
	}
	return c.dispatcher.Call(ctx, "thread/start", params, threadCallTimeout(call.Timeout), CallMetadata{
		TaskID:           call.TaskID,
		SessionGroupID:   c.session.SessionGroupID,
		CloseOnUncertain: true,
	})
}

func (c *Connection) ResumeThread(ctx context.Context, call ThreadResumeCall) (json.RawMessage, error) {
	if !c.policy.CanResume() {
		return nil, schemaUnverifiedError(c.session.SessionGroupID, call.TaskID, call.ThreadID)
	}
	return c.dispatcher.Call(ctx, "thread/resume", NewThreadResumeParams(call.ThreadID), threadCallTimeout(call.Timeout), CallMetadata{
		TaskID:           call.TaskID,
		SessionGroupID:   c.session.SessionGroupID,
		ExpectedThreadID: call.ThreadID,
		CloseOnUncertain: true,
	})
}

func (c *Connection) StartTurn(ctx context.Context, call TurnStartCall) (json.RawMessage, error) {
	params := NewTurnStartParams(call.ThreadID, call.ClientUserMessageID, call.Input)
	return c.dispatcher.Call(ctx, "turn/start", params, turnStartTimeout(call.Timeout), CallMetadata{
		TaskID:           call.TaskID,
		SessionGroupID:   c.session.SessionGroupID,
		ExpectedThreadID: call.ThreadID,
		CloseOnUncertain: true,
	})
}

func (c *Connection) InterruptTurn(ctx context.Context, call TurnInterruptCall) (json.RawMessage, error) {
	return c.dispatcher.Call(ctx, "turn/interrupt", NewTurnInterruptParams(call.TurnID), turnInterruptTimeout(call.Timeout), CallMetadata{
		TaskID:           call.TaskID,
		SessionGroupID:   c.session.SessionGroupID,
		ExpectedTurnID:   call.TurnID,
		CloseOnUncertain: true,
	})
}

func (c *Connection) Initialize(ctx context.Context) error {
	result, err := c.dispatcher.Call(ctx, "initialize", initializeParams(), initializeTimeout, CallMetadata{SessionGroupID: c.session.SessionGroupID})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return unavailable(domain.ReasonDispatcherUnavailable, "initialize failed", c.session.SessionGroupID)
	}
	codexHome, err := parseInitializeCodexHome(result)
	if err != nil {
		return err
	}
	if err := VerifyCodexHomeIdentity(c.session.CanonicalCodexHome, codexHome); err != nil {
		return &domain.GatewayError{
			Code: domain.GatewayErrorCodeFailedPrecondition,
			Details: domain.GatewayErrorDetails{
				Reason:         domain.ReasonCodexHomeMismatch,
				DisplayMessage: "app-server returned a different codex home",
				SessionGroupID: c.session.SessionGroupID,
			},
		}
	}
	if err := c.dispatcher.Notify("initialized", nil); err != nil {
		return unavailable(domain.ReasonDispatcherUnavailable, "initialized notification failed", c.session.SessionGroupID)
	}
	return nil
}

func (c *Connection) Call(ctx context.Context, method string, params any, timeout time.Duration, metadata CallMetadata) (json.RawMessage, error) {
	return c.dispatcher.Call(ctx, method, params, timeout, metadata)
}

func (c *Connection) Notify(method string, params any) error {
	return c.dispatcher.Notify(method, params)
}

func (c *Connection) RespondServerRequest(ctx context.Context, request ServerRequest, result any, timeout time.Duration) error {
	ctx, cancel := responseContext(ctx, timeout, defaultPendingResponseTimeout)
	defer cancel()
	return c.dispatcher.Respond(ctx, request.ID, result)
}

func (c *Connection) RespondServerRequestError(ctx context.Context, request ServerRequest, code int, message string, timeout time.Duration) error {
	ctx, cancel := responseContext(ctx, timeout, unsupportedRequestTimeout)
	defer cancel()
	return c.dispatcher.RespondError(ctx, request.ID, code, message)
}

func (c *Connection) Requests() <-chan ServerRequest {
	if c.requests == nil {
		return nil
	}
	return c.requests
}

func (c *Connection) Notifications() <-chan Notification {
	return c.dispatcher.Notifications()
}

func (c *Connection) Done() <-chan struct{} {
	return c.dispatcher.Done()
}

func (c *Connection) SensitiveRegistry() *redact.Registry {
	if c == nil {
		return nil
	}
	return c.sensitive
}

func (c *Connection) SessionGroupID() string {
	if c == nil {
		return ""
	}
	return c.session.SessionGroupID
}

func (c *Connection) Close() error {
	if c.onClose != nil {
		c.onClose()
	}
	err := c.dispatcher.Close()
	if c.waitClose != nil {
		c.waitClose()
	}
	return err
}

func (c *Connection) handleServerRequests() {
	for request := range c.dispatcher.Requests() {
		switch request.Method {
		case "account/chatgptAuthTokens/refresh":
			c.handleAuthRefresh(request)
		case "attestation/generate", "item/tool/call", "execCommandApproval", "applyPatchApproval":
			c.respondUnsupported(request)
		default:
			if c.requests != nil {
				select {
				case c.requests <- request:
				case <-c.dispatcher.Done():
				}
			} else {
				c.respondUnsupported(request)
			}
		}
	}
	if c.requests != nil {
		close(c.requests)
	}
}

func (c *Connection) handleAuthRefresh(request ServerRequest) {
	params, err := parseCredentialRefreshParams(request.Params)
	if err != nil {
		c.failAuthRefresh(request)
		return
	}
	if c.credentialProvider == nil {
		c.failAuthRefresh(request)
		return
	}

	providerCtx, cancelProvider := context.WithCancel(context.Background())
	defer cancelProvider()
	go func() {
		select {
		case <-c.dispatcher.Done():
			cancelProvider()
		case <-providerCtx.Done():
		}
	}()
	if c.dispatcherClosed() {
		return
	}

	response, err := InvokeCredentialProvider(providerCtx, *c.credentialProvider, CredentialRefreshRequestV1{
		SchemaVersion:     1,
		SessionGroupID:    c.session.SessionGroupID,
		Reason:            params.Reason,
		PreviousAccountID: params.PreviousAccountID,
	}, c.providerEnv, c.sensitive)
	if err != nil {
		if c.dispatcherClosed() {
			return
		}
		c.failAuthRefresh(request)
		return
	}
	if c.dispatcherClosed() {
		return
	}
	_ = c.dispatcher.Respond(context.Background(), request.ID, map[string]any{
		"accessToken":      response.AccessToken,
		"chatgptAccountId": response.ChatGPTAccountID,
		"chatgptPlanType":  response.ChatGPTPlanType,
	})
}

func (c *Connection) failAuthRefresh(request ServerRequest) {
	if c.dispatcherClosed() {
		return
	}
	_ = c.dispatcher.RespondError(context.Background(), request.ID, authRefreshUnavailableCode, authRefreshUnavailableMessage)
	activeTaskID, onAuthRefreshFailure := c.authRefreshFailureHooks()
	if onAuthRefreshFailure == nil {
		return
	}
	taskID := request.TaskID
	if taskID == "" && activeTaskID != nil {
		taskID = activeTaskID()
	}
	onAuthRefreshFailure(AuthRefreshFailure{
		SessionGroupID: c.session.SessionGroupID,
		TaskID:         taskID,
		Reason:         domain.ReasonAuthRefreshUnavailable,
	})
}

func (c *Connection) authRefreshFailureHooks() (ActiveTaskIDFunc, AuthRefreshFailureHook) {
	c.hooksMu.RLock()
	defer c.hooksMu.RUnlock()
	return c.activeTaskID, c.onAuthRefreshFailure
}

func (c *Connection) unsupportedServerRequestHook() UnsupportedServerRequestHook {
	c.hooksMu.RLock()
	defer c.hooksMu.RUnlock()
	return c.onUnsupportedRequest
}

func (c *Connection) dispatcherClosed() bool {
	select {
	case <-c.dispatcher.Done():
		return true
	default:
		return false
	}
}

func (c *Connection) respondUnsupported(request ServerRequest) {
	hook := c.unsupportedServerRequestHook()
	taskID := request.TaskID
	threadID := ParseThreadID(request.Params)
	turnID := ParseTurnID(request.Params)

	ctx, cancel := context.WithTimeout(context.Background(), unsupportedRequestTimeout)
	defer cancel()
	_ = c.dispatcher.RespondError(ctx, request.ID, unsupportedServerRequestCode, "unsupported_server_request")
	if hook == nil {
		return
	}
	hook(UnsupportedServerRequest{
		SessionGroupID: c.session.SessionGroupID,
		TaskID:         taskID,
		ThreadID:       threadID,
		TurnID:         turnID,
		Method:         request.Method,
	})
}

type initializeClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeCapabilities struct {
	ExperimentalAPI    bool `json:"experimentalApi"`
	RequestAttestation bool `json:"requestAttestation"`
}

type initializeRequest struct {
	ClientInfo   initializeClientInfo   `json:"clientInfo"`
	Capabilities initializeCapabilities `json:"capabilities"`
}

type initializeResponse struct {
	CodexHome string `json:"codexHome"`
}

type credentialRefreshParams struct {
	Reason            string  `json:"reason"`
	PreviousAccountID *string `json:"previousAccountId"`
}

func initializeParams() initializeRequest {
	return initializeRequest{
		ClientInfo: initializeClientInfo{
			Name:    "codex-control-gateway",
			Version: "0.1.0",
		},
		Capabilities: initializeCapabilities{
			ExperimentalAPI:    true,
			RequestAttestation: false,
		},
	}
}

func parseInitializeCodexHome(raw json.RawMessage) (string, error) {
	var response initializeResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", fmt.Errorf("parse initialize response: %w", err)
	}
	if response.CodexHome == "" {
		return "", fmt.Errorf("initialize response missing codexHome")
	}
	return response.CodexHome, nil
}

func parseCredentialRefreshParams(raw json.RawMessage) (credentialRefreshParams, error) {
	if len(raw) == 0 {
		return credentialRefreshParams{}, fmt.Errorf("auth refresh params missing")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var params credentialRefreshParams
	if err := decoder.Decode(&params); err != nil {
		return credentialRefreshParams{}, fmt.Errorf("parse auth refresh params: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return credentialRefreshParams{}, fmt.Errorf("auth refresh params have trailing data")
	}
	if params.Reason != authRefreshReasonUnauthorized {
		return credentialRefreshParams{}, fmt.Errorf("auth refresh reason is unsupported")
	}
	if params.PreviousAccountID != nil && invalidCredentialField(*params.PreviousAccountID, maxProviderAccountBytes) {
		return credentialRefreshParams{}, fmt.Errorf("auth refresh previous account id is invalid")
	}
	return params, nil
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	copied := value
	return &copied
}

func threadCallTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}
	return defaultThreadCallTimeout
}

func turnStartTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}
	return defaultTurnStartTimeout
}

func turnInterruptTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}
	return defaultTurnInterruptTimeout
}

func responseContext(ctx context.Context, timeout time.Duration, fallback time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	if timeout <= 0 {
		timeout = fallback
	}
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func schemaUnverifiedError(sessionGroupID string, taskID string, threadID string) *domain.GatewayError {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeFailedPrecondition,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonAppServerSchemaUnverified,
			DisplayMessage: "app-server schema is unverified for this flow",
			SessionGroupID: sessionGroupID,
			TaskID:         taskID,
			ThreadID:       threadID,
			Retryable:      false,
		},
	}
}

func unavailable(reason domain.GatewayErrorReason, message string, sessionGroupID string) *domain.GatewayError {
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeUnavailable,
		Details: domain.GatewayErrorDetails{
			Reason:         reason,
			DisplayMessage: message,
			SessionGroupID: sessionGroupID,
			Retryable:      true,
		},
	}
}
