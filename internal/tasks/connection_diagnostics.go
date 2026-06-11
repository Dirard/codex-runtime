package tasks

import (
	"github.com/Dirard/codex-runtime/internal/appserver"
	"github.com/Dirard/codex-runtime/internal/domain"
	"github.com/Dirard/codex-runtime/internal/redact"
)

const connectionDiagnosticsLimit = 256

type connectionDiagnostic struct {
	CreatedAtUnixMS int64
	SessionGroupID  string
	TaskID          string
	Code            string
	Method          string
	RequestType     string
	AutoResolution  string
	LimitReason     string
}

func (s *Service) recordConnectionDiagnostic(diagnostic connectionDiagnostic) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordConnectionDiagnosticLocked(diagnostic)
}

func (s *Service) recordConnectionDiagnosticForConnection(diagnostic connectionDiagnostic, connection *appserver.Connection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordConnectionDiagnosticWithRedactorLocked(diagnostic, connectionDiagnosticRedactor(connection))
}

func (s *Service) recordConnectionDiagnosticLocked(diagnostic connectionDiagnostic) {
	s.recordConnectionDiagnosticWithRedactorLocked(diagnostic, redact.New())
}

func (s *Service) recordConnectionDiagnosticWithRedactorLocked(diagnostic connectionDiagnostic, redactor *redact.Redactor) {
	if diagnostic.CreatedAtUnixMS == 0 {
		diagnostic.CreatedAtUnixMS = s.now().UnixMilli()
	}
	diagnostic = sanitizeConnectionDiagnostic(diagnostic, redactor)
	if len(s.connectionDiagnostics) >= connectionDiagnosticsLimit {
		copy(s.connectionDiagnostics, s.connectionDiagnostics[1:])
		s.connectionDiagnostics[len(s.connectionDiagnostics)-1] = diagnostic
		return
	}
	s.connectionDiagnostics = append(s.connectionDiagnostics, diagnostic)
}

func (s *Service) connectionDiagnosticsSnapshot() []connectionDiagnostic {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]connectionDiagnostic(nil), s.connectionDiagnostics...)
}

func connectionDiagnosticRedactor(connection *appserver.Connection) *redact.Redactor {
	if connection == nil {
		return redact.New()
	}
	return redact.New(redact.WithConnectionRegistry(connection.SensitiveRegistry()))
}

func sanitizeConnectionDiagnostic(diagnostic connectionDiagnostic, redactor *redact.Redactor) connectionDiagnostic {
	if redactor == nil {
		redactor = redact.New()
	}
	diagnostic.SessionGroupID = publicTextWithRedactor(redactor, diagnostic.SessionGroupID, domain.MaxPublicIDBytes, "")
	diagnostic.TaskID = publicTextWithRedactor(redactor, diagnostic.TaskID, domain.MaxPublicIDBytes, "")
	diagnostic.Code = publicTextWithRedactor(redactor, diagnostic.Code, domain.MaxSourceLabelBytes, "diagnostic")
	diagnostic.Method = publicTextWithRedactor(redactor, diagnostic.Method, domain.MaxSourceLabelBytes, "")
	diagnostic.RequestType = publicTextWithRedactor(redactor, diagnostic.RequestType, domain.MaxSourceLabelBytes, "")
	diagnostic.AutoResolution = publicTextWithRedactor(redactor, diagnostic.AutoResolution, domain.MaxSourceLabelBytes, "")
	diagnostic.LimitReason = publicTextWithRedactor(redactor, diagnostic.LimitReason, domain.MaxSourceLabelBytes, "")
	return diagnostic
}
