package pending

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/Dirard/codex-runtime/internal/domain"
)

const (
	MethodCommandApproval     = "item/commandExecution/requestApproval"
	MethodFileChangeApproval  = "item/fileChange/requestApproval"
	MethodPermissionsApproval = "item/permissions/requestApproval"
	MethodMcpElicitation      = "mcpServer/elicitation/request"
	MethodToolUserInput       = "item/tool/requestUserInput"

	WarningCodeOverLimit                = "pending_request_over_limit"
	WarningCodeUnsupportedServerRequest = "unsupported_server_request"

	LimitReasonDisplayPayloadTooLarge  = "display_payload_too_large"
	LimitReasonControlsTooLarge        = "controls_too_large"
	LimitReasonUnsafePrivilegeMetadata = "unsafe_privilege_metadata"
	LimitReasonStatusBudgetExceeded    = "status_budget_exceeded"

	RequestTypeCommandApproval     = "command_approval"
	RequestTypeFileApproval        = "file_approval"
	RequestTypePermissionsApproval = "permissions_approval"
	RequestTypeMcpElicitation      = "mcp_elicitation"
	RequestTypeToolUserInput       = "tool_user_input"

	AutoResolutionDecline      = "decline"
	AutoResolutionDenyAll      = "deny_all"
	AutoResolutionJSONRPCError = "jsonrpc_error"

	ToolUserInputOverLimitCode    = -32003
	ToolUserInputOverLimitMessage = "pending_request_over_limit"

	UnsupportedServerRequestCode    = -32002
	UnsupportedServerRequestMessage = "unsupported_server_request"

	UnsupportedReasonAdvancedDecision         = "advanced_decision_out_of_mvp"
	UnsupportedReasonSecurityUnrepresentable  = "approval_security_metadata_unrepresentable"
	UnsupportedReasonGrantRootUnrepresentable = "grant_root_unrepresentable"
)

var ErrOverLimit = errors.New("pending request over limit")

type ResponseState string

const (
	ResponseStateResponding ResponseState = "responding"
	ResponseStateAccepted   ResponseState = "accepted"
	ResponseStateFailed     ResponseState = "failed"
)

type Manager struct {
	nextSeq           uint64
	requests          map[string]*Record
	activeByServerID  map[string][]string
	activeByJSONRPCID map[string][]string
}

type Record struct {
	Method             string
	AppServerRequestID string
	JSONRPCID          json.RawMessage
	Active             bool
	Pending            domain.PendingRequest

	ApprovalOptions    map[string]domain.ApprovalDecisionOption
	PermissionGrants   map[string]PermissionGrant
	ToolQuestions      map[string]ToolQuestionSpec
	McpSensitiveFields map[string]struct{}

	Responses                map[string]*ResponseEntry
	InFlightClientResponseID string
}

type PermissionGrant struct {
	Kind      string
	Grantable bool
	Section   string
	Field     string
	Value     any
}

type ToolQuestionSpec struct {
	IsSecret      bool
	IsOther       bool
	AllowedValues map[string]struct{}
}

type ResponseEntry struct {
	ClientResponseID string
	Fingerprint      string
	State            ResponseState
	Done             chan struct{}
	Response         domain.RespondPendingRequestResponse
	Err              error
}

type BuildInput struct {
	TaskID            string
	ThreadID          string
	TurnID            string
	CreatedAtUnixMS   int64
	RedactString      func(string, int, string) string
	SanitizePathLabel func(string) (string, bool)
	CachedFileDiff    *domain.FileDiffUpdatedEvent
}

type BuildResult struct {
	Record      *Record
	RequestType string
	LimitReason string
}

func NewManager() *Manager {
	return &Manager{
		requests:          map[string]*Record{},
		activeByServerID:  map[string][]string{},
		activeByJSONRPCID: map[string][]string{},
	}
}

func (m *Manager) Add(record *Record) string {
	if m.requests == nil {
		m.requests = map[string]*Record{}
	}
	if m.activeByServerID == nil {
		m.activeByServerID = map[string][]string{}
	}
	if m.activeByJSONRPCID == nil {
		m.activeByJSONRPCID = map[string][]string{}
	}
	m.nextSeq++
	pendingID := "pending-" + strconvUint(m.nextSeq)
	record.Pending.PendingRequestID = pendingID
	record.Pending.Display = cloneDisplay(record.Pending.Display)
	record.Active = true
	if record.Responses == nil {
		record.Responses = map[string]*ResponseEntry{}
	}
	m.requests[pendingID] = record
	if record.AppServerRequestID != "" {
		m.activeByServerID[record.AppServerRequestID] = append(m.activeByServerID[record.AppServerRequestID], pendingID)
	}
	if jsonrpcID := RequestIDKey(record.JSONRPCID); jsonrpcID != "" {
		m.activeByJSONRPCID[jsonrpcID] = append(m.activeByJSONRPCID[jsonrpcID], pendingID)
	}
	return pendingID
}

func (m *Manager) Get(pendingRequestID string) *Record {
	if m == nil {
		return nil
	}
	return m.requests[pendingRequestID]
}

func (m *Manager) Active() []domain.PendingRequest {
	if m == nil {
		return nil
	}
	active := make([]domain.PendingRequest, 0, len(m.requests))
	for _, record := range m.requests {
		if record.Active {
			active = append(active, record.Pending)
		}
	}
	sort.Slice(active, func(i, j int) bool {
		if active[i].CreatedAtUnixMS == active[j].CreatedAtUnixMS {
			return active[i].PendingRequestID < active[j].PendingRequestID
		}
		return active[i].CreatedAtUnixMS < active[j].CreatedAtUnixMS
	})
	return active
}

func (m *Manager) ActiveCount() int {
	if m == nil {
		return 0
	}
	count := 0
	for _, record := range m.requests {
		if record.Active {
			count++
		}
	}
	return count
}

func (m *Manager) ActiveByServerRequestID(serverRequestID string) *Record {
	if m == nil || serverRequestID == "" {
		return nil
	}
	return m.activeByIndex(m.activeByServerID, serverRequestID)
}

func (m *Manager) ActiveByResolvedServerRequestID(jsonrpcID json.RawMessage) *Record {
	if m == nil {
		return nil
	}
	jsonrpcIDKey := RequestIDKey(jsonrpcID)
	if jsonrpcIDKey == "" {
		return nil
	}
	if record := m.activeByIndex(m.activeByJSONRPCID, jsonrpcIDKey); record != nil {
		return record
	}
	exactRawSeen := false
	for _, record := range m.requests {
		if record == nil || RequestIDKey(record.JSONRPCID) != jsonrpcIDKey {
			continue
		}
		if record.Active {
			return record
		}
		exactRawSeen = true
	}
	if exactRawSeen {
		return nil
	}
	return m.ActiveByServerRequestID(NormalizeServerRequestID("", jsonrpcID))
}

func (m *Manager) MarkResolved(record *Record) {
	if m == nil || record == nil {
		return
	}
	record.Active = false
	if record.AppServerRequestID != "" {
		removeActiveID(m.activeByServerID, record.AppServerRequestID, record.Pending.PendingRequestID)
	}
	if jsonrpcID := RequestIDKey(record.JSONRPCID); jsonrpcID != "" {
		removeActiveID(m.activeByJSONRPCID, jsonrpcID, record.Pending.PendingRequestID)
	}
}

func (m *Manager) activeByIndex(index map[string][]string, key string) *Record {
	if m == nil || key == "" {
		return nil
	}
	for _, pendingID := range index[key] {
		record := m.requests[pendingID]
		if record != nil && record.Active {
			return record
		}
	}
	return nil
}

func removeActiveID(index map[string][]string, key string, pendingID string) {
	ids := index[key]
	if len(ids) == 0 {
		return
	}
	for i, id := range ids {
		if id != pendingID {
			continue
		}
		copy(ids[i:], ids[i+1:])
		ids = ids[:len(ids)-1]
		if len(ids) == 0 {
			delete(index, key)
			return
		}
		index[key] = ids
		return
	}
}

func MethodPendingType(method string) (domain.PendingType, bool) {
	switch method {
	case MethodCommandApproval:
		return domain.PendingTypeCommandApproval, true
	case MethodFileChangeApproval:
		return domain.PendingTypeFileChangeApproval, true
	case MethodPermissionsApproval:
		return domain.PendingTypePermissionsApproval, true
	case MethodMcpElicitation:
		return domain.PendingTypeMcpElicitation, true
	case MethodToolUserInput:
		return domain.PendingTypeToolUserInput, true
	default:
		return "", false
	}
}

func RequestTypeForPendingType(pendingType domain.PendingType) string {
	switch pendingType {
	case domain.PendingTypeCommandApproval:
		return RequestTypeCommandApproval
	case domain.PendingTypeFileChangeApproval:
		return RequestTypeFileApproval
	case domain.PendingTypePermissionsApproval:
		return RequestTypePermissionsApproval
	case domain.PendingTypeMcpElicitation:
		return RequestTypeMcpElicitation
	case domain.PendingTypeToolUserInput:
		return RequestTypeToolUserInput
	default:
		return string(pendingType)
	}
}

func AutoResolutionForPendingType(pendingType domain.PendingType) string {
	switch pendingType {
	case domain.PendingTypeCommandApproval, domain.PendingTypeFileChangeApproval, domain.PendingTypeMcpElicitation:
		return AutoResolutionDecline
	case domain.PendingTypePermissionsApproval:
		return AutoResolutionDenyAll
	case domain.PendingTypeToolUserInput:
		return AutoResolutionJSONRPCError
	default:
		return AutoResolutionDecline
	}
}

func RequestIDKey(id json.RawMessage) string {
	return string(bytesTrimSpace(id))
}

func DisplayPayloadBytes(display domain.PendingRequestDisplay) int {
	raw, err := json.Marshal(display)
	if err != nil {
		return domain.MaxOutboundPendingDisplayPayloadBytes + 1
	}
	return len(raw)
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func cloneDisplay(display domain.PendingRequestDisplay) domain.PendingRequestDisplay {
	return display
}

func bytesTrimSpace(raw []byte) []byte {
	start := 0
	for start < len(raw) && isSpace(raw[start]) {
		start++
	}
	end := len(raw)
	for end > start && isSpace(raw[end-1]) {
		end--
	}
	return raw[start:end]
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\n' || b == '\r' || b == '\t'
}

func strconvUint(value uint64) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}

func NormalizeServerRequestID(rawID string, jsonrpcID json.RawMessage) string {
	if strings.TrimSpace(rawID) != "" {
		return rawID
	}
	var stringID string
	if json.Unmarshal(jsonrpcID, &stringID) == nil && strings.TrimSpace(stringID) != "" {
		return stringID
	}
	return RequestIDKey(jsonrpcID)
}
