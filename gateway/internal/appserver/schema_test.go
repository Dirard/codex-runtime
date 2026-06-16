package appserver

import (
	"testing"

	"github.com/Dirard/codex-runtime/gateway/internal/domain"
)

func TestVendoredSchemaFixturesContainRequiredFields(t *testing.T) {
	fixture, err := loadVendoredSchemaFixtureMetadata()
	if err != nil {
		t.Fatalf("load schema metadata: %v", err)
	}
	start, err := loadSchemaProperties("ThreadStartParams", fixture.SourceSchemaSHA256)
	if err != nil {
		t.Fatalf("load ThreadStartParams schema: %v", err)
	}
	resumeParams, err := loadSchemaProperties("ThreadResumeParams", fixture.SourceSchemaSHA256)
	if err != nil {
		t.Fatalf("load ThreadResumeParams schema: %v", err)
	}
	resumeResponse, err := loadSchemaProperties("ThreadResumeResponse", fixture.SourceSchemaSHA256)
	if err != nil {
		t.Fatalf("load ThreadResumeResponse schema: %v", err)
	}

	required := map[string]bool{
		"ThreadStartParams.permissions":                start.hasProperty("permissions"),
		"ThreadResumeParams.excludeTurns":              resumeParams.hasProperty("excludeTurns"),
		"ThreadResumeResponse.initialTurnsPage":        resumeResponse.hasProperty("initialTurnsPage"),
		"ThreadResumeResponse.activePermissionProfile": resumeResponse.hasProperty("activePermissionProfile"),
	}
	for name, present := range required {
		if !present {
			t.Fatalf("vendored experimental schema fixture missing %s", name)
		}
	}
}

func TestLoadVendoredSchemaMetadataRequiredFields(t *testing.T) {
	metadata, err := LoadVendoredSchemaMetadata()
	if err != nil {
		t.Fatalf("LoadVendoredSchemaMetadata() error = %v", err)
	}
	requireCompleteSchemaMetadata(t, metadata)
	if metadata.ExperimentalFilteringApplied {
		t.Fatal("vendored metadata is filtered, want experimental-aware")
	}
	if metadata.TargetCodexVersion != "codex 0.137.0" {
		t.Fatalf("TargetCodexVersion = %q, want codex 0.137.0", metadata.TargetCodexVersion)
	}
	if metadata.GeneratorVersion != "codex-app-server-protocol 0.137.0 write_schema_fixtures --experimental" {
		t.Fatalf("GeneratorVersion = %q, want generated experimental schema provenance", metadata.GeneratorVersion)
	}
	for _, name := range []string{"ThreadStartParams", "ThreadResumeParams", "ThreadResumeResponse"} {
		if metadata.SourceSchemaSHA256[name] == "" {
			t.Fatalf("metadata missing source schema hash for %s", name)
		}
	}
}

func TestSchemaMetadataCompletenessDoesNotRequireExperimentalFields(t *testing.T) {
	metadata := mustLoadVendoredSchemaMetadata(t)
	metadata.ThreadStartPermissions = false

	if err := metadata.ValidateCompleteness(); err != nil {
		t.Fatalf("ValidateCompleteness() error = %v", err)
	}
	if err := metadata.ValidateRequiredFields(); err == nil {
		t.Fatal("ValidateRequiredFields() succeeded with missing required field")
	}
}

func TestSchemaPolicyWarnModeAndStrictMode(t *testing.T) {
	metadata := mustLoadVendoredSchemaMetadata(t)
	metadata.ThreadResumeExcludeTurns = false

	policy, err := NewSchemaPolicy(metadata, metadata.TargetCodexVersion, false)
	if err != nil {
		t.Fatalf("NewSchemaPolicy(warn) error = %v", err)
	}
	if policy.CanResume() {
		t.Fatal("CanResume() = true, want false when required resume fields are missing")
	}
	diagnostic := policy.RuntimeDiagnostic()
	if diagnostic == nil || diagnostic.Details.Reason != domain.ReasonAppServerSchemaUnverified {
		t.Fatalf("RuntimeDiagnostic() = %#v, want app_server_schema_unverified", diagnostic)
	}

	if _, err := NewSchemaPolicy(metadata, metadata.TargetCodexVersion, true); err == nil {
		t.Fatal("NewSchemaPolicy(strict) succeeded with missing required field")
	}
}

func TestSchemaPolicyRuntimeVersionMismatchDiagnostic(t *testing.T) {
	metadata := mustLoadVendoredSchemaMetadata(t)
	policy, err := NewSchemaPolicy(metadata, "newer-runtime", false)
	if err != nil {
		t.Fatalf("NewSchemaPolicy() error = %v", err)
	}
	if diagnostic := policy.RuntimeDiagnostic(); diagnostic == nil || diagnostic.Details.Reason != domain.ReasonAppServerSchemaUnverified {
		t.Fatalf("RuntimeDiagnostic() = %#v, want app_server_schema_unverified", diagnostic)
	}
}

func mustLoadVendoredSchemaMetadata(t *testing.T) SchemaMetadata {
	t.Helper()
	metadata, err := LoadVendoredSchemaMetadata()
	if err != nil {
		t.Fatalf("LoadVendoredSchemaMetadata() error = %v", err)
	}
	requireCompleteSchemaMetadata(t, metadata)
	return metadata
}

func requireCompleteSchemaMetadata(t *testing.T, metadata SchemaMetadata) {
	t.Helper()
	if err := metadata.ValidateRequiredFields(); err != nil {
		t.Fatalf("ValidateRequiredFields() error = %v", err)
	}
	for _, name := range []string{"ThreadStartParams", "ThreadResumeParams", "ThreadResumeResponse"} {
		if metadata.SourceSchemaSHA256[name] == "" {
			t.Fatalf("metadata missing source schema hash for %s", name)
		}
	}
}
