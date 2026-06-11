package appserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/Dirard/codex-runtime/internal/config"
	"github.com/Dirard/codex-runtime/internal/redact"
)

const (
	credentialSchemaVersion = 1
	maxProviderTokenBytes   = 64 * 1024
	maxProviderAccountBytes = 2 * 1024
	maxProviderPlanBytes    = 512
)

type CredentialRefreshRequestV1 struct {
	SchemaVersion     int     `json:"schemaVersion"`
	SessionGroupID    string  `json:"sessionGroupId"`
	Reason            string  `json:"reason"`
	PreviousAccountID *string `json:"previousAccountId"`
}

type CredentialRefreshResponseV1 struct {
	SchemaVersion    int     `json:"schemaVersion"`
	AccessToken      string  `json:"accessToken"`
	ChatGPTAccountID string  `json:"chatgptAccountId"`
	ChatGPTPlanType  *string `json:"chatgptPlanType"`
}

type credentialRefreshResponseV1JSON struct {
	SchemaVersion    json.RawMessage `json:"schemaVersion"`
	AccessToken      json.RawMessage `json:"accessToken"`
	ChatGPTAccountID json.RawMessage `json:"chatgptAccountId"`
	ChatGPTPlanType  json.RawMessage `json:"chatgptPlanType"`
}

func InvokeCredentialProvider(
	ctx context.Context,
	provider config.CredentialProvider,
	request CredentialRefreshRequestV1,
	parentEnv map[string]string,
	registry *redact.Registry,
) (CredentialRefreshResponseV1, error) {
	if request.SchemaVersion == 0 {
		request.SchemaVersion = credentialSchemaVersion
	}
	if request.SchemaVersion != credentialSchemaVersion {
		return CredentialRefreshResponseV1{}, fmt.Errorf("unsupported credential request schema")
	}

	timeout := time.Duration(provider.TimeoutMillis) * time.Millisecond
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = 10 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stdin, err := json.Marshal(request)
	if err != nil {
		return CredentialRefreshResponseV1{}, err
	}
	stdin = append(stdin, '\n')

	if provider.CanonicalExecutable == "" {
		return CredentialRefreshResponseV1{}, fmt.Errorf("credential provider canonical executable is required")
	}
	command := exec.CommandContext(callCtx, provider.CanonicalExecutable, provider.Args...)
	if provider.CanonicalWorkdir != "" {
		command.Dir = provider.CanonicalWorkdir
	}
	command.Env = providerEnv(provider.EnvSources, parentEnv)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdin = bytes.NewReader(stdin)
	command.Stdout = limitWriter(&stdout, int(provider.StdoutBytes))
	command.Stderr = limitWriter(&stderr, int(provider.StderrBytes))

	err = command.Run()
	if callCtx.Err() != nil {
		return CredentialRefreshResponseV1{}, callCtx.Err()
	}
	if err != nil {
		return CredentialRefreshResponseV1{}, fmt.Errorf("credential provider failed")
	}
	if stdout.Len() > int(provider.StdoutBytes) || stderr.Len() > int(provider.StderrBytes) {
		return CredentialRefreshResponseV1{}, fmt.Errorf("credential provider output exceeded limit")
	}

	response, err := parseCredentialProviderResponse(stdout.Bytes())
	if err != nil {
		return CredentialRefreshResponseV1{}, err
	}
	if registry != nil {
		registry.Add(response.AccessToken)
		registry.Add(response.ChatGPTAccountID)
		if response.ChatGPTPlanType != nil {
			registry.Add(*response.ChatGPTPlanType)
		}
	}
	return response, nil
}

func parseCredentialProviderResponse(data []byte) (CredentialRefreshResponseV1, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var rawResponse credentialRefreshResponseV1JSON
	if err := decoder.Decode(&rawResponse); err != nil {
		return CredentialRefreshResponseV1{}, fmt.Errorf("credential provider returned invalid JSON")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return CredentialRefreshResponseV1{}, fmt.Errorf("credential provider returned trailing data")
	}
	if len(rawResponse.ChatGPTPlanType) == 0 {
		return CredentialRefreshResponseV1{}, fmt.Errorf("credential provider plan type is required")
	}

	var response CredentialRefreshResponseV1
	if err := unmarshalCredentialField(rawResponse.SchemaVersion, &response.SchemaVersion); err != nil {
		return CredentialRefreshResponseV1{}, err
	}
	if err := unmarshalCredentialField(rawResponse.AccessToken, &response.AccessToken); err != nil {
		return CredentialRefreshResponseV1{}, err
	}
	if err := unmarshalCredentialField(rawResponse.ChatGPTAccountID, &response.ChatGPTAccountID); err != nil {
		return CredentialRefreshResponseV1{}, err
	}
	if err := json.Unmarshal(rawResponse.ChatGPTPlanType, &response.ChatGPTPlanType); err != nil {
		return CredentialRefreshResponseV1{}, fmt.Errorf("credential provider returned invalid JSON")
	}
	if response.SchemaVersion != credentialSchemaVersion {
		return CredentialRefreshResponseV1{}, fmt.Errorf("credential provider schema mismatch")
	}
	if invalidCredentialField(response.AccessToken, maxProviderTokenBytes) {
		return CredentialRefreshResponseV1{}, fmt.Errorf("credential provider access token is invalid")
	}
	if invalidCredentialField(response.ChatGPTAccountID, maxProviderAccountBytes) {
		return CredentialRefreshResponseV1{}, fmt.Errorf("credential provider account id is invalid")
	}
	if response.ChatGPTPlanType != nil && invalidCredentialField(*response.ChatGPTPlanType, maxProviderPlanBytes) {
		return CredentialRefreshResponseV1{}, fmt.Errorf("credential provider plan type is invalid")
	}
	return response, nil
}

func unmarshalCredentialField(data json.RawMessage, value any) error {
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("credential provider returned invalid JSON")
	}
	return nil
}

func invalidCredentialField(value string, maxBytes int) bool {
	if value == "" || len(value) > maxBytes {
		return true
	}
	return strings.ContainsAny(value, "\r\n\x00")
}

func providerEnv(names []string, parent map[string]string) []string {
	env := make([]string, 0, len(names))
	for _, name := range names {
		if value, ok := lookupEnv(parent, name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}

func providerEnvMap(names []string, parent map[string]string) map[string]string {
	env := map[string]string{}
	for _, name := range names {
		if value, ok := lookupEnv(parent, name); ok {
			env[name] = value
		}
	}
	if len(env) == 0 {
		return nil
	}
	return env
}

func limitWriter(buffer *bytes.Buffer, limit int) io.Writer {
	return writerFunc(func(p []byte) (int, error) {
		remaining := limit + 1 - buffer.Len()
		if remaining <= 0 {
			return len(p), nil
		}
		if len(p) > remaining {
			_, _ = buffer.Write(p[:remaining])
			return len(p), nil
		}
		_, _ = buffer.Write(p)
		return len(p), nil
	})
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
}
