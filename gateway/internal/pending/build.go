package pending

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	redactpkg "github.com/Dirard/codex-runtime/gateway/internal/redact"
)

func Build(method string, params json.RawMessage, jsonrpcID json.RawMessage, input BuildInput) (BuildResult, error) {
	pendingType, ok := MethodPendingType(method)
	if !ok {
		return BuildResult{}, fmt.Errorf("unsupported pending method %q", method)
	}

	root := decodeObject(params)
	record := &Record{
		Method:             method,
		AppServerRequestID: NormalizeServerRequestID(firstString(root, "requestId", "requestID"), jsonrpcID),
		JSONRPCID:          cloneRaw(jsonrpcID),
		ApprovalOptions:    map[string]domain.ApprovalDecisionOption{},
		PermissionGrants:   map[string]PermissionGrant{},
		ToolQuestions:      map[string]ToolQuestionSpec{},
		Responses:          map[string]*ResponseEntry{},
	}
	record.Pending = domain.PendingRequest{
		TaskID:          input.TaskID,
		PendingType:     pendingType,
		CreatedAtUnixMS: input.CreatedAtUnixMS,
		ThreadID:        firstString(root, "threadId", "thread.id"),
		TurnID:          firstString(root, "turnId", "turn.id"),
		ItemID:          itemID(root),
	}
	if record.Pending.ThreadID == "" {
		record.Pending.ThreadID = input.ThreadID
	}
	if record.Pending.TurnID == "" {
		record.Pending.TurnID = input.TurnID
	}

	var limitReason string
	switch pendingType {
	case domain.PendingTypeCommandApproval:
		limitReason = buildCommandApproval(record, root, input)
	case domain.PendingTypeFileChangeApproval:
		limitReason = buildFileChangeApproval(record, root, input)
	case domain.PendingTypePermissionsApproval:
		limitReason = buildPermissionsApproval(record, root, input)
	case domain.PendingTypeMcpElicitation:
		limitReason = buildMcpElicitation(record, root, input)
	case domain.PendingTypeToolUserInput:
		limitReason = buildToolUserInput(record, root, input)
	default:
		return BuildResult{}, fmt.Errorf("unsupported pending type %q", pendingType)
	}
	if limitReason != "" {
		return BuildResult{
			Record:      record,
			RequestType: RequestTypeForPendingType(pendingType),
			LimitReason: limitReason,
		}, ErrOverLimit
	}
	if record.Pending.Display == nil {
		return BuildResult{
			Record:      record,
			RequestType: RequestTypeForPendingType(pendingType),
			LimitReason: LimitReasonControlsTooLarge,
		}, ErrOverLimit
	}
	return BuildResult{
		Record:      record,
		RequestType: RequestTypeForPendingType(pendingType),
	}, nil
}

func buildCommandApproval(record *Record, root map[string]any, input BuildInput) string {
	redact := input.RedactString
	display := domain.CommandApprovalDisplay{
		CommandDisplay:  public(redact, commandDisplay(root), domain.MaxOutboundCommandDisplayBytes, "command"),
		WorkspaceLabel:  pathLabel(input.SanitizePathLabel, redact, firstString(root, "workspaceLabel", "cwd", "workdir"), ""),
		Reason:          public(redact, firstString(root, "reason", "description", "message"), domain.MaxOutboundErrorDisplayMessageBytes, ""),
		DecisionOptions: nil,
	}

	security, blocking, limitReason := approvalSecurity(root, input)
	if limitReason != "" {
		return limitReason
	}
	display.ApprovalSecurity = security

	options, limitReason := commandDecisionOptions(root, blocking)
	if limitReason != "" {
		return limitReason
	}
	display.DecisionOptions = options
	for _, option := range options {
		if option.Selectable && option.WireDecision != "" {
			record.ApprovalOptions[option.DecisionID] = option
		}
	}
	record.Pending.Display = display
	return ""
}

func buildFileChangeApproval(record *Record, root map[string]any, input BuildInput) string {
	redact := input.RedactString
	display := domain.FileChangeApprovalDisplay{
		FileLabel:       pathLabel(input.SanitizePathLabel, redact, firstString(root, "fileLabel", "path", "filePath", "change.path", "item.path"), "file"),
		ChangeKind:      public(redact, firstString(root, "changeKind", "action", "change.kind"), domain.MaxSourceLabelBytes, ""),
		DiffSummary:     public(redact, firstString(root, "diffSummary", "summary", "diff.summary"), domain.MaxOutboundDiffDisplayBytes, ""),
		DiffUnified:     public(redact, firstString(root, "diffUnified", "unifiedDiff", "patch", "diff.unified", "diff"), domain.MaxOutboundDiffDisplayBytes, ""),
		DecisionOptions: nil,
	}
	if display.FileLabel == "file" {
		display.FileLabel = fileLabel(root, input)
	}
	if input.CachedFileDiff != nil {
		display.FileLabel = input.CachedFileDiff.FileLabel
		display.ChangeKind = input.CachedFileDiff.ChangeKind
		display.DiffSummary = input.CachedFileDiff.DiffSummary
		display.DiffUnified = input.CachedFileDiff.DiffUnified
	} else if display.DiffSummary == "" && display.DiffUnified == "" {
		display.DiffUnavailable = true
	}

	grantRoot, blocking := fileGrantRoot(root, input)
	display.GrantRoot = grantRoot
	options := directDecisionOptions([]domain.ApprovalWireDecision{
		domain.ApprovalWireDecisionAccept,
		domain.ApprovalWireDecisionAcceptForSession,
		domain.ApprovalWireDecisionDecline,
		domain.ApprovalWireDecisionCancel,
	}, blocking)
	display.DecisionOptions = options
	for _, option := range options {
		if option.Selectable && option.WireDecision != "" {
			record.ApprovalOptions[option.DecisionID] = option
		}
	}
	record.Pending.Display = display
	return ""
}

func buildPermissionsApproval(record *Record, root map[string]any, input BuildInput) string {
	redact := input.RedactString
	permissionsRoot := objectValue(firstValue(root, "permissions", "requestedPermissions", "permissionProfile"))
	atoms := make([]domain.PermissionAtom, 0)

	addGrant := func(kind string, displayLabel string, scopeLabel string, grantable bool, reason string, section string, field string, value any) {
		permissionID := fmt.Sprintf("permission-%d", len(atoms)+1)
		if grantable && len(record.PermissionGrants) < domain.MaxPermissionAtoms {
			record.PermissionGrants[permissionID] = PermissionGrant{
				Kind:      kind,
				Grantable: true,
				Section:   section,
				Field:     field,
				Value:     cloneJSONValue(value),
			}
		}
		atoms = append(atoms, domain.PermissionAtom{
			PermissionID:      permissionID,
			Kind:              public(redact, kind, domain.MaxSourceLabelBytes, "permission"),
			DisplayLabel:      public(redact, displayLabel, domain.MaxOutboundPendingDisplayStringBytes, "permission"),
			ScopeLabel:        public(redact, scopeLabel, domain.MaxSourceLabelBytes, ""),
			Grantable:         grantable,
			UngrantableReason: public(redact, reason, domain.MaxOutboundErrorDisplayMessageBytes, ""),
		})
	}

	if network := objectValue(firstValue(permissionsRoot, "network")); network != nil {
		if enabled, ok := boolValue(firstValue(network, "enabled")); ok {
			addGrant("network", "network access", "", enabled, reasonIfFalse(enabled, "network permission is disabled"), "network", "enabled", enabled)
		}
	}
	fileSystem := objectValue(firstValue(permissionsRoot, "fileSystem", "filesystem", "fs"))
	addPathPermissions(addGrant, fileSystem, input, "entries")
	addPathPermissions(addGrant, fileSystem, input, "read")
	addPathPermissions(addGrant, fileSystem, input, "write")
	addPathPermissions(addGrant, permissionsRoot, input, "entries")
	addPathPermissions(addGrant, permissionsRoot, input, "read")
	addPathPermissions(addGrant, permissionsRoot, input, "write")

	if len(atoms) > domain.MaxPermissionAtoms {
		return LimitReasonControlsTooLarge
	}
	display := domain.PermissionsApprovalDisplay{
		RequestedPermissions: atoms,
		RecommendedScope:     recommendedScope(root),
		Reason:               public(redact, firstString(root, "reason", "message", "description"), domain.MaxOutboundErrorDisplayMessageBytes, ""),
	}
	record.Pending.Display = display
	return ""
}

func buildMcpElicitation(record *Record, root map[string]any, input BuildInput) string {
	redact := input.RedactString
	schemaValue := firstValue(root, "schema", "formSchema", "inputSchema")
	record.McpSensitiveFields = sensitiveSchemaFields(schemaValue)
	schemaJSON, schemaTruncated := jsonDisplay(schemaValue, redact, domain.MaxOutboundMcpFormSchemaBytes)
	if schemaTruncated {
		return LimitReasonDisplayPayloadTooLarge
	}
	url, urlOK := mcpDisplayURL(firstString(root, "url", "href"), redact)
	if !urlOK {
		return LimitReasonDisplayPayloadTooLarge
	}
	mode := domain.ElicitationModeForm
	if url != "" {
		mode = domain.ElicitationModeURL
	}
	record.Pending.Display = domain.McpElicitationDisplay{
		Mode:           mode,
		Message:        public(redact, firstString(root, "message", "prompt", "description"), domain.MaxOutboundErrorDisplayMessageBytes, ""),
		FormSchemaJSON: schemaJSON,
		URL:            url,
		SubmitLabel:    public(redact, firstString(root, "submitLabel", "submit_label"), domain.MaxSourceLabelBytes, ""),
	}
	return ""
}

func mcpDisplayURL(raw string, redact func(string, int, string) string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", true
	}
	if unsafeMcpDisplayURL(raw) {
		return "", false
	}
	return public(redact, raw, domain.MaxSourceURIBytes, ""), true
}

func unsafeMcpDisplayURL(raw string) bool {
	if len(raw) > domain.MaxSourceURIBytes {
		return true
	}
	if looksLikeLocalPath(raw) {
		return true
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return true
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return true
	}
	if hasTraversalPathSegment(parsed.EscapedPath()) {
		return true
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "http", "https":
		return unsafeMcpDisplayHTTPHost(parsed)
	default:
		return true
	}
}

func unsafeMcpDisplayHTTPHost(parsed *url.URL) bool {
	if parsed == nil || parsed.Host == "" {
		return true
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" || strings.Contains(host, "%") {
		return true
	}
	host = strings.ToLower(strings.TrimRight(host, "."))
	if host == "" {
		return true
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return unsafeMcpDisplayIP(addr)
	}
	if strings.Contains(host, ":") || legacyIPv4LikeHost(host) {
		return true
	}
	return localStyleHostname(host)
}

func unsafeMcpDisplayIP(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsLoopback() ||
		addr.IsUnspecified() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast()
}

func legacyIPv4LikeHost(host string) bool {
	labels := strings.Split(host, ".")
	if len(labels) == 1 {
		addr, ok := legacyIPv4SingleAddress(host)
		return ok && unsafeMcpDisplayIP(addr)
	}
	if len(labels) > 4 {
		return false
	}
	for _, label := range labels {
		if !ipv4NumericLabel(label) {
			return false
		}
	}
	return true
}

func ipv4NumericLabel(label string) bool {
	if label == "" {
		return false
	}
	if allASCIIDigits(label) {
		return true
	}
	_, ok := parseLegacyIPv4Number(label)
	return ok
}

func legacyIPv4SingleAddress(host string) (netip.Addr, bool) {
	value, ok := parseLegacyIPv4Number(host)
	if !ok || value > uint64(^uint32(0)) {
		return netip.Addr{}, false
	}
	return netip.AddrFrom4([4]byte{
		byte(value >> 24),
		byte(value >> 16),
		byte(value >> 8),
		byte(value),
	}), true
}

func parseLegacyIPv4Number(value string) (uint64, bool) {
	if value == "" {
		return 0, false
	}
	base := 10
	digits := value
	if strings.HasPrefix(value, "0x") {
		base = 16
		digits = value[2:]
	} else if len(value) > 1 && value[0] == '0' {
		base = 8
		digits = value[1:]
	}
	if digits == "" || !validDigitsForBase(digits, base) {
		return 0, false
	}
	parsed, err := strconv.ParseUint(digits, base, 64)
	return parsed, err == nil
}

func validDigitsForBase(value string, base int) bool {
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			if int(r-'0') >= base {
				return false
			}
		case base == 16 && r >= 'a' && r <= 'f':
		case base == 16 && r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func allASCIIDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}

func localStyleHostname(host string) bool {
	if !strings.Contains(host, ".") {
		return true
	}
	return host == "localhost" ||
		strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".localdomain")
}

func buildToolUserInput(record *Record, root map[string]any, input BuildInput) string {
	redact := input.RedactString
	rawQuestions, _ := firstValue(root, "questions").([]any)
	if len(rawQuestions) == 0 || len(rawQuestions) > domain.MaxToolUserInputQuestions {
		return LimitReasonControlsTooLarge
	}
	questions := make([]domain.ToolUserInputQuestion, 0, len(rawQuestions))
	seenQuestionIDs := make(map[string]struct{}, len(rawQuestions))
	for _, rawQuestion := range rawQuestions {
		questionRoot, ok := rawQuestion.(map[string]any)
		if !ok {
			return LimitReasonControlsTooLarge
		}
		id := firstString(questionRoot, "id", "questionId", "questionID")
		if id == "" || len(id) > domain.MaxPublicIDBytes || strings.TrimSpace(id) != id {
			return LimitReasonControlsTooLarge
		}
		if _, ok := seenQuestionIDs[id]; ok {
			return LimitReasonControlsTooLarge
		}
		seenQuestionIDs[id] = struct{}{}
		isSecret, _ := boolValue(firstValue(questionRoot, "isSecret", "secret"))
		isOther, _ := boolValue(firstValue(questionRoot, "isOther", "other"))
		options, allowedValues, limitReason := toolOptions(questionRoot, redact)
		if limitReason != "" {
			return limitReason
		}
		record.ToolQuestions[id] = ToolQuestionSpec{
			IsSecret:      isSecret,
			IsOther:       isOther,
			AllowedValues: allowedValues,
		}
		questions = append(questions, domain.ToolUserInputQuestion{
			ID:       id,
			Header:   public(redact, firstString(questionRoot, "header", "label", "title", "name"), domain.MaxSourceLabelBytes, ""),
			Question: public(redact, firstString(questionRoot, "question", "prompt", "message", "label"), domain.MaxOutboundErrorDisplayMessageBytes, ""),
			IsOther:  isOther,
			IsSecret: isSecret,
			Options:  options,
		})
	}
	record.Pending.Display = domain.ToolUserInputDisplay{Questions: questions}
	return ""
}

func commandDecisionOptions(root map[string]any, blockingReason string) ([]domain.ApprovalDecisionOption, string) {
	rawDecisions, found := decisionValues(root)
	if !found {
		return directDecisionOptions([]domain.ApprovalWireDecision{
			domain.ApprovalWireDecisionAccept,
			domain.ApprovalWireDecisionAcceptForSession,
			domain.ApprovalWireDecisionDecline,
			domain.ApprovalWireDecisionCancel,
		}, blockingReason), ""
	}
	options := make([]domain.ApprovalDecisionOption, 0, len(rawDecisions)+2)
	seenDirect := map[domain.ApprovalWireDecision]struct{}{}
	for _, rawDecision := range rawDecisions {
		if decision, ok := directDecision(rawDecision); ok {
			if _, ok := seenDirect[decision]; ok {
				return nil, LimitReasonControlsTooLarge
			}
			seenDirect[decision] = struct{}{}
			options = append(options, directDecisionOption(decision, blockingReason))
			continue
		}
		options = append(options, domain.ApprovalDecisionOption{
			DecisionID:        fmt.Sprintf("advanced-%d", len(options)+1),
			DisplayLabel:      "advanced approval",
			Summary:           "advanced approval is not supported by this gateway",
			Selectable:        false,
			UnsupportedReason: UnsupportedReasonAdvancedDecision,
		})
	}
	for _, decision := range []domain.ApprovalWireDecision{domain.ApprovalWireDecisionDecline, domain.ApprovalWireDecisionCancel} {
		if _, ok := seenDirect[decision]; !ok {
			options = append(options, directDecisionOption(decision, blockingReason))
		}
	}
	if len(options) > domain.MaxApprovalDecisionOptions {
		return nil, LimitReasonControlsTooLarge
	}
	return options, ""
}

func directDecisionOptions(decisions []domain.ApprovalWireDecision, blockingReason string) []domain.ApprovalDecisionOption {
	options := make([]domain.ApprovalDecisionOption, 0, len(decisions))
	for _, decision := range decisions {
		options = append(options, directDecisionOption(decision, blockingReason))
	}
	return options
}

func directDecisionOption(decision domain.ApprovalWireDecision, blockingReason string) domain.ApprovalDecisionOption {
	selectable := true
	unsupportedReason := ""
	if blockingReason != "" && (decision == domain.ApprovalWireDecisionAccept || decision == domain.ApprovalWireDecisionAcceptForSession) {
		selectable = false
		unsupportedReason = blockingReason
	}
	return domain.ApprovalDecisionOption{
		DecisionID:        "decision-" + string(decision),
		WireDecision:      decision,
		DisplayLabel:      decisionLabel(decision),
		Selectable:        selectable,
		UnsupportedReason: unsupportedReason,
	}
}

func approvalSecurity(root map[string]any, input BuildInput) (*domain.ApprovalSecurityMetadata, string, string) {
	redact := input.RedactString
	var metadata domain.ApprovalSecurityMetadata
	entries := 0
	blockingReason := ""

	if network := objectValue(firstValue(root, "networkApprovalContext", "networkContext")); network != nil {
		metadata.HasPrivilegeExpansion = true
		metadata.NetworkContext = &domain.NetworkContextDisplay{
			HostLabel: approvalSecurityHostLabel(redact, firstString(network, "host", "hostname", "hostLabel", "url", "origin")),
			Protocol:  public(redact, firstString(network, "protocol", "scheme"), domain.MaxSourceLabelBytes, ""),
		}
		entries++
	}
	additional := objectValue(firstValue(root, "additionalPermissions", "additional_permissions"))
	if additional != nil {
		metadata.HasPrivilegeExpansion = true
		if network := objectValue(firstValue(additional, "network")); network != nil {
			if enabled, ok := boolValue(firstValue(network, "enabled")); ok {
				metadata.AdditionalNetwork = &domain.AdditionalNetworkDisplay{Enabled: enabled}
				entries++
			}
		}
		fileSystem := objectValue(firstValue(additional, "fileSystem", "filesystem", "fs"))
		for _, field := range []string{"entries", "read", "write"} {
			for _, entry := range pathPermissionValues(fileSystem, field) {
				label, underCWD := pathEntryLabel(entry, input)
				approvable := underCWD
				reason := ""
				if !approvable {
					reason = "path is outside configured cwd"
					blockingReason = UnsupportedReasonSecurityUnrepresentable
				}
				metadata.AdditionalFilesystemEntries = append(metadata.AdditionalFilesystemEntries, domain.AdditionalFilesystemEntry{
					EntryID:            fmt.Sprintf("fs-%d", len(metadata.AdditionalFilesystemEntries)+1),
					Access:             public(redact, accessForPathPermission(field, entry), domain.MaxSourceLabelBytes, field),
					PathLabel:          public(redact, label, domain.MaxSourceLabelBytes, "[REDACTED:path]"),
					Approvable:         approvable,
					UnapprovableReason: reason,
				})
				entries++
			}
		}
	}
	if execPolicy := firstValue(root, "proposedExecpolicyAmendment", "proposedExecPolicyAmendment"); execPolicy != nil {
		metadata.HasPrivilegeExpansion = true
		summary, truncated := truncate(public(redact, commandValueDisplay(execPolicy), domain.MaxOutboundCommandDisplayBytes, "command"), domain.MaxOutboundCommandDisplayBytes)
		metadata.ExecPolicyAmendmentSummary = &domain.ExecPolicyAmendmentSummary{
			CommandDisplay: summary,
			Truncated:      truncated,
		}
		entries++
	}
	if amendments, ok := firstValue(root, "proposedNetworkPolicyAmendments").([]any); ok {
		metadata.HasPrivilegeExpansion = true
		for _, amendment := range amendments {
			amendmentRoot := objectValue(amendment)
			metadata.NetworkPolicyAmendmentSummaries = append(metadata.NetworkPolicyAmendmentSummaries, domain.NetworkPolicyAmendmentSummary{
				HostLabel:  approvalSecurityHostLabel(redact, firstString(amendmentRoot, "host", "hostLabel", "hostname")),
				Action:     public(redact, firstString(amendmentRoot, "action", "type"), domain.MaxSourceLabelBytes, "network"),
				Approvable: false,
			})
			blockingReason = UnsupportedReasonSecurityUnrepresentable
			entries++
		}
	}
	if entries > domain.MaxApprovalSecurityMetadataEntries {
		return nil, "", LimitReasonControlsTooLarge
	}
	if !metadata.HasPrivilegeExpansion {
		return nil, "", ""
	}
	if blockingReason != "" {
		metadata.BlockingReason = blockingReason
	}
	return &metadata, blockingReason, ""
}

func approvalSecurityHostLabel(redact func(string, int, string) string, raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "network"
	}
	return public(redact, "network", domain.MaxSourceLabelBytes, "network")
}

func fileGrantRoot(root map[string]any, input BuildInput) (*domain.FileGrantRootDisplay, string) {
	raw := firstString(root, "grantRoot", "grant_root")
	if raw == "" {
		return nil, ""
	}
	label, underCWD := sanitizePath(input.SanitizePathLabel, raw)
	display := &domain.FileGrantRootDisplay{
		Present:            true,
		RootLabel:          label,
		UnderConfiguredCWD: underCWD,
		Approvable:         underCWD,
	}
	if !underCWD {
		display.UnapprovableReason = "grant root is outside configured cwd"
		return display, UnsupportedReasonGrantRootUnrepresentable
	}
	return display, ""
}

type pathGrantAppender func(kind string, displayLabel string, scopeLabel string, grantable bool, reason string, section string, field string, value any)

func addPathPermissions(addGrant pathGrantAppender, root map[string]any, input BuildInput, field string) {
	for _, value := range pathPermissionValues(root, field) {
		label, underCWD := pathEntryLabel(value, input)
		access := accessForPathPermission(field, value)
		grantable := underCWD
		reason := ""
		if !grantable {
			reason = "path is outside configured cwd"
		}
		addGrant("filesystem", label, access, grantable, reason, "fileSystem", field, value)
	}
}

func pathPermissionValues(root map[string]any, field string) []any {
	if root == nil {
		return nil
	}
	raw := firstValue(root, field)
	switch typed := raw.(type) {
	case []any:
		return typed
	case []map[string]any:
		values := make([]any, 0, len(typed))
		for _, value := range typed {
			values = append(values, value)
		}
		return values
	case map[string]any:
		return []any{typed}
	case string:
		return []any{typed}
	default:
		return nil
	}
}

func pathEntryLabel(value any, input BuildInput) (string, bool) {
	switch typed := value.(type) {
	case string:
		return sanitizePath(input.SanitizePathLabel, typed)
	case map[string]any:
		if nestedPath := objectValue(firstValue(typed, "path")); nestedPath != nil {
			return pathObjectLabel(nestedPath, input)
		}
		if path := firstPathString(typed, "path.path"); path != "" {
			return sanitizePath(input.SanitizePathLabel, path)
		}
		if path := firstPathString(typed, "path"); path != "" {
			return sanitizePath(input.SanitizePathLabel, path)
		}
	}
	return "[REDACTED:path]", false
}

func pathObjectLabel(root map[string]any, input BuildInput) (string, bool) {
	pathType := strings.ToLower(strings.TrimSpace(firstString(root, "type")))
	switch pathType {
	case "", "path", "literal":
		if path := firstPathString(root, "path"); path != "" {
			return sanitizePath(input.SanitizePathLabel, path)
		}
		if path := firstPathString(root, "value"); path != "" && pathType != "" {
			return sanitizePath(input.SanitizePathLabel, path)
		}
	}
	return "[REDACTED:path]", false
}

func firstPathString(root map[string]any, paths ...string) string {
	for _, path := range paths {
		value, ok := valueAtPath(root, path)
		if !ok {
			continue
		}
		text, ok := value.(string)
		if ok && text != "" {
			return text
		}
	}
	return ""
}

func sanitizePath(sanitize func(string) (string, bool), raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "[REDACTED:path]", false
	}
	if sanitize != nil {
		label, ok := sanitize(raw)
		if label != "" {
			return label, ok
		}
		return "[REDACTED:path]", false
	}
	return "[REDACTED:path]", false
}

func looksLikeLocalPath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	normalized := strings.ReplaceAll(value, `\`, "/")
	if strings.HasPrefix(normalized, "/") ||
		strings.HasPrefix(normalized, "~/") ||
		normalized == "~" {
		return true
	}
	if hasWindowsDrivePrefix(normalized) {
		return true
	}
	if strings.HasPrefix(normalized, "//") {
		return true
	}
	return false
}

func hasWindowsDrivePrefix(value string) bool {
	if len(value) < 2 || value[1] != ':' {
		return false
	}
	first := value[0]
	return (first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z')
}

func accessForPathPermission(field string, value any) string {
	if root := objectValue(value); root != nil {
		if access := firstString(root, "access", "mode"); access != "" {
			return access
		}
	}
	return field
}

func toolOptions(root map[string]any, redact func(string, int, string) string) ([]string, map[string]struct{}, string) {
	rawOptions, _ := firstValue(root, "options", "choices").([]any)
	if len(rawOptions) > domain.MaxToolUserInputOptionsPerQuestion {
		return nil, nil, LimitReasonControlsTooLarge
	}
	options := make([]string, 0, len(rawOptions))
	allowedValues := map[string]struct{}{}
	for _, rawOption := range rawOptions {
		token, ok := toolOptionPublicToken(rawOption)
		if !ok {
			return nil, nil, LimitReasonControlsTooLarge
		}
		if !sendableToolOptionToken(token, redact) {
			return nil, nil, LimitReasonControlsTooLarge
		}
		if _, ok := allowedValues[token]; ok {
			return nil, nil, LimitReasonControlsTooLarge
		}
		allowedValues[token] = struct{}{}
		options = append(options, token)
	}
	if len(allowedValues) == 0 {
		allowedValues = nil
	}
	return options, allowedValues, ""
}

func sendableToolOptionToken(token string, redact func(string, int, string) string) bool {
	if strings.TrimSpace(token) == "" || len(token) > domain.MaxSourceLabelBytes {
		return false
	}
	if unsafePublicOptionToken(token) {
		return false
	}
	redacted := redactpkg.New().RedactString(token)
	if redact != nil {
		redacted = redact(token, domain.MaxSourceLabelBytes, "")
	}
	if redacted != token {
		return false
	}
	return true
}

func unsafePublicOptionToken(token string) bool {
	value := strings.TrimSpace(token)
	if value == "" {
		return false
	}
	if hasTraversalPathSegment(value) || looksLikeLocalPath(value) {
		return true
	}
	if hasScheme, unsafe := publicOptionTokenSchemeSafety(value); hasScheme {
		return unsafe
	}
	if looksLikeRelativePublicOptionPath(value) {
		return true
	}
	return looksLikeLocalPublicOptionHost(value)
}

func hasTraversalPathSegment(value string) bool {
	const maxPathUnescapeDepth = 4

	current := value
	for depth := 0; depth < maxPathUnescapeDepth; depth++ {
		if hasTraversalPathSegmentNormalized(current) {
			return true
		}
		unescaped, err := url.PathUnescape(current)
		if err != nil || unescaped == current {
			return false
		}
		current = unescaped
	}
	if hasTraversalPathSegmentNormalized(current) {
		return true
	}
	unescaped, err := url.PathUnescape(current)
	return err == nil && unescaped != current
}

func hasTraversalPathSegmentNormalized(value string) bool {
	normalized := strings.ReplaceAll(value, `\`, "/")
	for _, segment := range strings.Split(normalized, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

func publicOptionTokenSchemeSafety(value string) (bool, bool) {
	parsed, err := url.Parse(value)
	if err != nil {
		return strings.Contains(value, "://"), true
	}
	if parsed.Scheme == "" {
		return false, false
	}
	return true, unsafeMcpDisplayURL(value)
}

func looksLikeRelativePublicOptionPath(value string) bool {
	normalized := strings.ReplaceAll(value, `\`, "/")
	return normalized == "." ||
		strings.HasPrefix(normalized, "./") ||
		strings.Contains(normalized, "/")
}

func looksLikeLocalPublicOptionHost(value string) bool {
	host := publicOptionHostCandidate(value)
	if host == "" {
		return false
	}
	if strings.Contains(host, "%") {
		return true
	}
	host = strings.ToLower(strings.TrimRight(host, "."))
	if host == "" {
		return false
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return unsafeMcpDisplayIP(addr)
	}
	return legacyIPv4LikeHost(host) ||
		host == "localhost" ||
		strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".localdomain")
}

func publicOptionHostCandidate(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "[") {
		if host, _, err := net.SplitHostPort(value); err == nil {
			return strings.Trim(host, "[]")
		}
		if end := strings.Index(value, "]"); end > 0 {
			return value[1:end]
		}
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		return host
	}
	if strings.Count(value, ":") == 1 {
		host, _, _ := strings.Cut(value, ":")
		return host
	}
	return value
}

func toolOptionPublicToken(rawOption any) (string, bool) {
	switch typed := rawOption.(type) {
	case string:
		return typed, true
	case map[string]any:
		if value, ok := valueAtPath(typed, "value"); ok {
			token, ok := value.(string)
			return token, ok && strings.TrimSpace(token) != ""
		}
		if value, ok := valueAtPath(typed, "id"); ok {
			token, ok := value.(string)
			return token, ok && strings.TrimSpace(token) != ""
		}
		if token := firstString(typed, "label", "title", "name"); token != "" {
			return token, true
		}
	}
	return "", false
}

func sensitiveSchemaFields(schema any) map[string]struct{} {
	fields := map[string]struct{}{}
	collectSensitiveSchemaFields(schema, "", fields)
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func collectSensitiveSchemaFields(schema any, fieldName string, fields map[string]struct{}) {
	root := objectValue(schema)
	if root == nil {
		return
	}
	if fieldName != "" && (redactpkg.IsSensitiveName(fieldName) || schemaMarksSensitive(root)) {
		fields[strings.ToLower(fieldName)] = struct{}{}
	}
	if properties := objectValue(firstValue(root, "properties")); properties != nil {
		for name, child := range properties {
			collectSensitiveSchemaFields(child, name, fields)
		}
	}
	if items := firstValue(root, "items"); items != nil {
		collectSensitiveSchemaFields(items, fieldName, fields)
	}
	if anyOf, ok := firstValue(root, "anyOf").([]any); ok {
		for _, child := range anyOf {
			collectSensitiveSchemaFields(child, fieldName, fields)
		}
	}
	if oneOf, ok := firstValue(root, "oneOf").([]any); ok {
		for _, child := range oneOf {
			collectSensitiveSchemaFields(child, fieldName, fields)
		}
	}
	if allOf, ok := firstValue(root, "allOf").([]any); ok {
		for _, child := range allOf {
			collectSensitiveSchemaFields(child, fieldName, fields)
		}
	}
}

func schemaMarksSensitive(schema map[string]any) bool {
	if strings.EqualFold(firstString(schema, "format"), "password") {
		return true
	}
	if writeOnly, ok := boolValue(firstValue(schema, "writeOnly")); ok && writeOnly {
		return true
	}
	if secret, ok := boolValue(firstValue(schema, "x-secret", "x_secret")); ok && secret {
		return true
	}
	return false
}

func recommendedScope(root map[string]any) domain.PermissionScope {
	switch strings.ToLower(firstString(root, "recommendedScope", "scope")) {
	case string(domain.PermissionScopeSession):
		return domain.PermissionScopeSession
	case string(domain.PermissionScopeTurn):
		return domain.PermissionScopeTurn
	default:
		return domain.PermissionScopeTurn
	}
}

func decisionValues(root map[string]any) ([]any, bool) {
	for _, path := range []string{"availableDecisions", "decisionOptions", "decisions", "options"} {
		value := firstValue(root, path)
		if value == nil {
			continue
		}
		switch typed := value.(type) {
		case []any:
			return typed, true
		case []string:
			values := make([]any, 0, len(typed))
			for _, value := range typed {
				values = append(values, value)
			}
			return values, true
		case string:
			return []any{typed}, true
		}
	}
	return nil, false
}

func directDecision(value any) (domain.ApprovalWireDecision, bool) {
	typed, ok := value.(string)
	if !ok {
		return "", false
	}
	return parseDecision(typed)
}

func parseDecision(value string) (domain.ApprovalWireDecision, bool) {
	switch domain.ApprovalWireDecision(value) {
	case domain.ApprovalWireDecisionAccept:
		return domain.ApprovalWireDecisionAccept, true
	case domain.ApprovalWireDecisionAcceptForSession:
		return domain.ApprovalWireDecisionAcceptForSession, true
	case domain.ApprovalWireDecisionDecline:
		return domain.ApprovalWireDecisionDecline, true
	case domain.ApprovalWireDecisionCancel:
		return domain.ApprovalWireDecisionCancel, true
	default:
		return "", false
	}
}

func decisionLabel(decision domain.ApprovalWireDecision) string {
	switch decision {
	case domain.ApprovalWireDecisionAccept:
		return "Accept"
	case domain.ApprovalWireDecisionAcceptForSession:
		return "Accept for session"
	case domain.ApprovalWireDecisionDecline:
		return "Decline"
	case domain.ApprovalWireDecisionCancel:
		return "Cancel"
	default:
		return "Decision"
	}
}

func fileLabel(root map[string]any, input BuildInput) string {
	for _, value := range pathPermissionValues(root, "changes") {
		label, ok := pathEntryLabel(value, input)
		if label != "" && ok {
			return label
		}
	}
	if label := firstString(root, "fileLabel", "path", "filePath"); label != "" {
		return pathLabel(input.SanitizePathLabel, input.RedactString, label, "file")
	}
	return "file"
}

func itemID(root map[string]any) string {
	return firstString(root, "itemId", "itemID", "item.id", "id")
}

func commandDisplay(root map[string]any) string {
	if display := firstString(root, "commandDisplay", "item.commandDisplay"); display != "" {
		return display
	}
	return commandValueDisplay(firstValue(root, "command", "item.command"))
}

func commandValueDisplay(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, part := range typed {
			parts = append(parts, fmt.Sprint(part))
		}
		return strings.Join(parts, " ")
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprint(value)
	}
}

func jsonDisplay(value any, redact func(string, int, string) string, maxBytes int) (string, bool) {
	if value == nil {
		return "", false
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", false
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return "", false
	}
	compactJSON := compact.String()
	if maxBytes > 0 && len(compactJSON) > maxBytes {
		return "", true
	}
	redactionCap := maxBytes
	if redactionCap > 0 {
		redactionCap++
	}
	return truncate(public(redact, compactJSON, redactionCap, ""), maxBytes)
}

func pathLabel(sanitize func(string) (string, bool), redact func(string, int, string) string, label string, fallback string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return fallback
	}
	if sanitize != nil {
		sanitized, _ := sanitizePath(sanitize, label)
		label = sanitized
	}
	return public(redact, label, domain.MaxSourceLabelBytes, fallback)
}

func public(redact func(string, int, string) string, value string, maxBytes int, fallback string) string {
	if redact != nil {
		return redact(value, maxBytes, fallback)
	}
	value, _ = truncate(value, maxBytes)
	if value == "" {
		return fallback
	}
	return value
}

func truncate(value string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value, false
	}
	truncated := value[:maxBytes]
	for !utf8.ValidString(truncated) && len(truncated) > 0 {
		truncated = truncated[:len(truncated)-1]
	}
	return truncated, true
}

func reasonIfFalse(ok bool, reason string) string {
	if ok {
		return ""
	}
	return reason
}

func decodeObject(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return map[string]any{}
	}
	return value
}

func firstString(root map[string]any, paths ...string) string {
	for _, path := range paths {
		value := firstValue(root, path)
		switch typed := value.(type) {
		case string:
			if typed != "" {
				return typed
			}
		case json.Number:
			return typed.String()
		case bool:
			return fmt.Sprint(typed)
		}
	}
	return ""
}

func firstValue(root map[string]any, paths ...string) any {
	for _, path := range paths {
		value, ok := valueAtPath(root, path)
		if ok && value != nil {
			return value
		}
	}
	return nil
}

func valueAtPath(root map[string]any, path string) (any, bool) {
	var current any = root
	for _, segment := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func objectValue(value any) map[string]any {
	if value == nil {
		return nil
	}
	typed, _ := value.(map[string]any)
	return typed
}

func boolValue(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(typed) {
		case "true":
			return true, true
		case "false":
			return false, true
		default:
			return false, false
		}
	default:
		return false, false
	}
}

func cloneJSONValue(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var cloned any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&cloned); err != nil {
		return value
	}
	return cloned
}
