package codex

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	pb "github.com/Dirard/codex-runtime/gen/codex/control/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	authorizationMetadataKey = "authorization"
	bearerPrefix             = "Bearer "
)

var (
	ErrDefaultClientNotConfigured = errors.New("codex: default client is not configured")
	ErrInvalidConfiguration       = errors.New("codex: invalid client configuration")
	ErrIDGeneration               = errors.New("codex: failed to generate request id")
	ErrNilClient                  = errors.New("codex: nil chat runtime client")
)

type Client struct {
	rpc            pb.ChatRuntimeServiceClient
	bearerToken    string
	sessionGroupID string
	workspaceID    string
	callOptions    []grpc.CallOption
}

type Option func(*clientConfig) error

type clientConfig struct {
	bearerToken    string
	sessionGroupID string
	workspaceID    string
	callOptions    []grpc.CallOption
}

func New(conn grpc.ClientConnInterface, opts ...Option) (*Client, error) {
	if conn == nil {
		return nil, fmt.Errorf("%w: grpc connection is required", ErrInvalidConfiguration)
	}
	return NewWithClient(pb.NewChatRuntimeServiceClient(conn), opts...)
}

func NewWithClient(rpc pb.ChatRuntimeServiceClient, opts ...Option) (*Client, error) {
	if rpc == nil {
		return nil, ErrNilClient
	}
	cfg := clientConfig{}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(cfg.bearerToken) == "" {
		return nil, fmt.Errorf("%w: bearer token is required", ErrInvalidConfiguration)
	}
	if strings.TrimSpace(cfg.sessionGroupID) == "" {
		return nil, fmt.Errorf("%w: session group id is required", ErrInvalidConfiguration)
	}
	if strings.TrimSpace(cfg.workspaceID) == "" {
		return nil, fmt.Errorf("%w: workspace id is required", ErrInvalidConfiguration)
	}
	return &Client{
		rpc:            rpc,
		bearerToken:    cfg.bearerToken,
		sessionGroupID: cfg.sessionGroupID,
		workspaceID:    cfg.workspaceID,
		callOptions:    append([]grpc.CallOption(nil), cfg.callOptions...),
	}, nil
}

func WithBearerToken(token string) Option {
	return func(cfg *clientConfig) error {
		if strings.TrimSpace(token) == "" {
			return fmt.Errorf("%w: bearer token is required", ErrInvalidConfiguration)
		}
		cfg.bearerToken = token
		return nil
	}
}

func WithSessionGroupID(sessionGroupID string) Option {
	return func(cfg *clientConfig) error {
		if strings.TrimSpace(sessionGroupID) == "" {
			return fmt.Errorf("%w: session group id is required", ErrInvalidConfiguration)
		}
		cfg.sessionGroupID = sessionGroupID
		return nil
	}
}

func WithWorkspaceID(workspaceID string) Option {
	return func(cfg *clientConfig) error {
		if strings.TrimSpace(workspaceID) == "" {
			return fmt.Errorf("%w: workspace id is required", ErrInvalidConfiguration)
		}
		cfg.workspaceID = workspaceID
		return nil
	}
}

func WithCallOptions(opts ...grpc.CallOption) Option {
	return func(cfg *clientConfig) error {
		cfg.callOptions = append(cfg.callOptions, opts...)
		return nil
	}
}

func (c *Client) runtimeContext() *pb.ChatRuntimeContext {
	return &pb.ChatRuntimeContext{
		SessionGroupId: c.sessionGroupID,
		WorkspaceId:    c.workspaceID,
	}
}

func (c *Client) authenticatedContext(ctx context.Context) (context.Context, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: context is nil", ErrInvalidConfiguration)
	}
	return metadata.NewOutgoingContext(ctx, metadata.Pairs(authorizationMetadataKey, bearerPrefix+c.bearerToken)), nil
}

func (c *Client) String() string {
	if c == nil {
		return "codex.Client<nil>"
	}
	return fmt.Sprintf("codex.Client{sessionGroupID:%q workspaceID:%q bearerToken:<redacted>}", c.sessionGroupID, c.workspaceID)
}

func (c *Client) GoString() string {
	return c.String()
}

var defaultClientStore struct {
	sync.RWMutex
	client *Client
}

func SetDefaultClient(client *Client) {
	defaultClientStore.Lock()
	defer defaultClientStore.Unlock()
	defaultClientStore.client = client
}

func DefaultClient() (*Client, bool) {
	defaultClientStore.RLock()
	defer defaultClientStore.RUnlock()
	if defaultClientStore.client == nil {
		return nil, false
	}
	return defaultClientStore.client, true
}

func Run(ctx context.Context, prompt string, opts ...RequestOption) (*Chat, *EventStream, error) {
	client, ok := DefaultClient()
	if !ok {
		return nil, nil, ErrDefaultClientNotConfigured
	}
	return client.Run(ctx, prompt, opts...)
}

func GetChat(ctx context.Context, chatID string) (*Chat, error) {
	client, ok := DefaultClient()
	if !ok {
		return nil, ErrDefaultClientNotConfigured
	}
	return client.GetChat(ctx, chatID)
}

type Error struct {
	Code             codes.Code
	Outcome          pb.ChatOutcomeCategory
	Reason           string
	DisplayMessage   string
	ChatID           string
	RunID            string
	SessionGroupID   string
	PendingRequestID string
	IdempotencyKey   string
	Retryable        bool

	err error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.DisplayMessage != "" {
		return e.DisplayMessage
	}
	if e.Reason != "" {
		return e.Reason
	}
	return e.Code.String()
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func AsError(err error) (*Error, bool) {
	if err == nil {
		return nil, false
	}
	var sdkErr *Error
	if errors.As(err, &sdkErr) {
		return sdkErr, true
	}
	return errorFromStatus(err)
}

func normalizeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return &Error{
			Code:           codes.Canceled,
			Outcome:        pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_CANCELLED,
			Reason:         "caller_canceled",
			DisplayMessage: "caller canceled",
			err:            err,
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{
			Code:           codes.DeadlineExceeded,
			Outcome:        pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_DEADLINE_EXCEEDED,
			Reason:         "caller_deadline_exceeded",
			DisplayMessage: "caller deadline exceeded",
			Retryable:      true,
			err:            err,
		}
	}
	if sdkErr, ok := errorFromStatus(err); ok {
		return sdkErr
	}
	return err
}

func newSDKError(code codes.Code, reason string, message string, retryable bool) *Error {
	if message == "" {
		message = reason
	}
	return &Error{
		Code:           code,
		Outcome:        outcomeFromCode(code),
		Reason:         reason,
		DisplayMessage: message,
		Retryable:      retryable,
	}
}

func newGeneratedPublicID(prefix string) (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("%w: %s", ErrIDGeneration, err)
	}
	return prefix + "-" + hex.EncodeToString(raw[:]), nil
}

func errorFromStatus(err error) (*Error, bool) {
	st, ok := status.FromError(err)
	if !ok {
		return nil, false
	}
	sdkErr := &Error{
		Code:           st.Code(),
		Outcome:        outcomeFromCode(st.Code()),
		Reason:         st.Message(),
		DisplayMessage: st.Message(),
		err:            err,
	}
	for _, detail := range st.Details() {
		switch typed := detail.(type) {
		case *pb.ChatRuntimeErrorDetails:
			sdkErr.Outcome = typed.GetOutcome()
			sdkErr.Reason = typed.GetReason()
			sdkErr.DisplayMessage = typed.GetDisplayMessage()
			sdkErr.ChatID = typed.GetChatId()
			sdkErr.RunID = typed.GetRunId()
			sdkErr.SessionGroupID = typed.GetSessionGroupId()
			sdkErr.PendingRequestID = typed.GetPendingRequestId()
			sdkErr.IdempotencyKey = typed.GetIdempotencyKey()
			sdkErr.Retryable = typed.GetRetryable()
			return sdkErr, true
		case *pb.GatewayErrorDetails:
			sdkErr.Reason = typed.GetReason()
			sdkErr.DisplayMessage = typed.GetDisplayMessage()
			sdkErr.ChatID = typed.GetThreadId()
			sdkErr.SessionGroupID = typed.GetSessionGroupId()
			sdkErr.PendingRequestID = typed.GetPendingRequestId()
			sdkErr.Retryable = typed.GetRetryable()
			return sdkErr, true
		}
	}
	return sdkErr, true
}

func outcomeFromCode(code codes.Code) pb.ChatOutcomeCategory {
	switch code {
	case codes.InvalidArgument:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_INVALID_ARGUMENT
	case codes.Unauthenticated:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_UNAUTHENTICATED
	case codes.PermissionDenied:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_PERMISSION_DENIED
	case codes.NotFound:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_NOT_FOUND
	case codes.FailedPrecondition:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_FAILED_PRECONDITION
	case codes.Aborted:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_ABORTED
	case codes.Unimplemented:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_UNSUPPORTED
	case codes.Unavailable:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_UNAVAILABLE
	case codes.Unknown:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_UNKNOWN
	case codes.OutOfRange:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_OUT_OF_RANGE
	case codes.DeadlineExceeded:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_DEADLINE_EXCEEDED
	case codes.Canceled:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_CANCELLED
	case codes.ResourceExhausted:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_UNAVAILABLE
	default:
		return pb.ChatOutcomeCategory_CHAT_OUTCOME_CATEGORY_INTERNAL
	}
}
