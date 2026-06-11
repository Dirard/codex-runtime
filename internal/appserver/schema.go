package appserver

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"

	"github.com/Dirard/codex-runtime/internal/domain"
)

//go:embed schemafixtures/experimental/*.json
var vendoredSchemaFixtures embed.FS

const vendoredExperimentalSchemaDir = "schemafixtures/experimental"

type SchemaMetadata struct {
	TargetCodexVersion                  string
	GeneratorVersion                    string
	ExperimentalFilteringApplied        bool
	SourceSchemaSHA256                  map[string]string
	ThreadStartPermissions              bool
	ThreadResumeExcludeTurns            bool
	ThreadResumeInitialTurnsPage        bool
	ThreadResumeActivePermissionProfile bool
}

type schemaMetadataFixture struct {
	TargetCodexVersion           string            `json:"targetCodexVersion"`
	GeneratorVersion             string            `json:"generatorVersion"`
	ExperimentalFilteringApplied bool              `json:"experimentalFilteringApplied"`
	SourceSchemaSHA256           map[string]string `json:"sourceSchemaSha256"`
}

type schemaPropertiesFixture struct {
	Properties map[string]json.RawMessage `json:"properties"`
}

type SchemaDiagnostic struct {
	Reason  domain.GatewayErrorReason
	Message string
}

type SchemaPolicy struct {
	Metadata       SchemaMetadata
	RuntimeVersion string
	Strict         bool
	diagnostics    []SchemaDiagnostic
}

func LoadVendoredSchemaMetadata() (SchemaMetadata, error) {
	fixture, err := loadVendoredSchemaFixtureMetadata()
	if err != nil {
		return SchemaMetadata{}, err
	}
	metadata := SchemaMetadata{
		TargetCodexVersion:           fixture.TargetCodexVersion,
		GeneratorVersion:             fixture.GeneratorVersion,
		ExperimentalFilteringApplied: fixture.ExperimentalFilteringApplied,
		SourceSchemaSHA256:           fixture.SourceSchemaSHA256,
	}
	if err := metadata.ValidateCompleteness(); err != nil {
		return SchemaMetadata{}, err
	}
	if err := populateRequiredFieldsFromVendoredSchemas(&metadata); err != nil {
		return SchemaMetadata{}, err
	}
	return metadata, nil
}

func NewSchemaPolicy(metadata SchemaMetadata, runtimeVersion string, strict bool) (SchemaPolicy, error) {
	policy := SchemaPolicy{
		Metadata:       metadata,
		RuntimeVersion: runtimeVersion,
		Strict:         strict,
	}
	policy.collectDiagnostics()
	if strict && len(policy.diagnostics) > 0 {
		return SchemaPolicy{}, policy.gatewayError()
	}
	return policy, nil
}

func (p SchemaPolicy) RuntimeDiagnostic() *domain.GatewayError {
	if len(p.diagnostics) == 0 {
		return nil
	}
	return p.gatewayError()
}

func (p SchemaPolicy) CanStartWithPermissionsProfile() bool {
	return p.Metadata.ThreadStartPermissions
}

func (p SchemaPolicy) CanResume() bool {
	return p.Metadata.ThreadResumeExcludeTurns &&
		p.Metadata.ThreadResumeInitialTurnsPage &&
		p.Metadata.ThreadResumeActivePermissionProfile
}

func (p *SchemaPolicy) collectDiagnostics() {
	if p.Metadata.ExperimentalFilteringApplied ||
		!p.Metadata.ThreadStartPermissions ||
		!p.Metadata.ThreadResumeExcludeTurns ||
		!p.Metadata.ThreadResumeInitialTurnsPage ||
		!p.Metadata.ThreadResumeActivePermissionProfile {
		p.diagnostics = append(p.diagnostics, SchemaDiagnostic{
			Reason:  domain.ReasonAppServerSchemaUnverified,
			Message: "app-server schema fixture is missing required experimental fields",
		})
	}
	if p.RuntimeVersion == "" || (p.Metadata.TargetCodexVersion != "" && p.RuntimeVersion != p.Metadata.TargetCodexVersion) {
		p.diagnostics = append(p.diagnostics, SchemaDiagnostic{
			Reason:  domain.ReasonAppServerSchemaUnverified,
			Message: "app-server runtime version is not verified against the schema fixture",
		})
	}
}

func (p SchemaPolicy) gatewayError() *domain.GatewayError {
	message := "app-server schema is unverified"
	if len(p.diagnostics) > 0 && p.diagnostics[0].Message != "" {
		message = p.diagnostics[0].Message
	}
	return &domain.GatewayError{
		Code: domain.GatewayErrorCodeFailedPrecondition,
		Details: domain.GatewayErrorDetails{
			Reason:         domain.ReasonAppServerSchemaUnverified,
			DisplayMessage: message,
			Retryable:      false,
		},
	}
}

func (m SchemaMetadata) ValidateRequiredFields() error {
	if m.ExperimentalFilteringApplied {
		return fmt.Errorf("schema metadata must be experimental-aware")
	}
	if err := m.ValidateCompleteness(); err != nil {
		return err
	}
	if !m.ThreadStartPermissions {
		return fmt.Errorf("ThreadStartParams.permissions missing")
	}
	if !m.ThreadResumeExcludeTurns {
		return fmt.Errorf("ThreadResumeParams.excludeTurns missing")
	}
	if !m.ThreadResumeInitialTurnsPage {
		return fmt.Errorf("ThreadResumeResponse.initialTurnsPage missing")
	}
	if !m.ThreadResumeActivePermissionProfile {
		return fmt.Errorf("ThreadResumeResponse.activePermissionProfile missing")
	}
	return nil
}

func (m SchemaMetadata) ValidateCompleteness() error {
	if len(m.SourceSchemaSHA256) == 0 || m.TargetCodexVersion == "" || m.GeneratorVersion == "" {
		return fmt.Errorf("schema metadata is incomplete")
	}
	return nil
}

func populateRequiredFieldsFromVendoredSchemas(metadata *SchemaMetadata) error {
	start, err := loadSchemaProperties("ThreadStartParams", metadata.SourceSchemaSHA256)
	if err != nil {
		return err
	}
	resumeParams, err := loadSchemaProperties("ThreadResumeParams", metadata.SourceSchemaSHA256)
	if err != nil {
		return err
	}
	resumeResponse, err := loadSchemaProperties("ThreadResumeResponse", metadata.SourceSchemaSHA256)
	if err != nil {
		return err
	}

	metadata.ThreadStartPermissions = start.hasProperty("permissions")
	metadata.ThreadResumeExcludeTurns = resumeParams.hasProperty("excludeTurns")
	metadata.ThreadResumeInitialTurnsPage = resumeResponse.hasProperty("initialTurnsPage")
	metadata.ThreadResumeActivePermissionProfile = resumeResponse.hasProperty("activePermissionProfile")
	return nil
}

func loadSchemaProperties(typeName string, sourceHashes map[string]string) (schemaPropertiesFixture, error) {
	filename := typeName + ".json"
	raw, err := readVendoredSchemaFixture(filename)
	if err != nil {
		return schemaPropertiesFixture{}, err
	}
	if err := verifySchemaFixtureHash(typeName, raw, sourceHashes[typeName]); err != nil {
		return schemaPropertiesFixture{}, err
	}

	var schema schemaPropertiesFixture
	if err := json.Unmarshal(raw, &schema); err != nil {
		return schemaPropertiesFixture{}, fmt.Errorf("parse %s: %w", filename, err)
	}
	return schema, nil
}

func loadVendoredSchemaFixtureMetadata() (schemaMetadataFixture, error) {
	raw, err := readVendoredSchemaFixture("metadata.json")
	if err != nil {
		return schemaMetadataFixture{}, err
	}
	var fixture schemaMetadataFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		return schemaMetadataFixture{}, err
	}
	return fixture, nil
}

func readVendoredSchemaFixture(filename string) ([]byte, error) {
	path := vendoredExperimentalSchemaDir + "/" + filename
	raw, err := vendoredSchemaFixtures.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read vendored schema fixture %s: %w", filename, err)
	}
	return raw, nil
}

func verifySchemaFixtureHash(typeName string, raw []byte, expected string) error {
	if expected == "" {
		return fmt.Errorf("schema metadata missing source hash for %s", typeName)
	}
	actual := fmt.Sprintf("%x", sha256.Sum256(raw))
	if actual != expected {
		return fmt.Errorf("schema fixture hash mismatch for %s", typeName)
	}
	return nil
}

func (s schemaPropertiesFixture) hasProperty(name string) bool {
	if len(s.Properties) == 0 {
		return false
	}
	_, ok := s.Properties[name]
	return ok
}
