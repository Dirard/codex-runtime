package appserver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/Dirard/codex-runtime/gateway/internal/config"
	"github.com/Dirard/codex-runtime/gateway/internal/domain"
	"github.com/Dirard/codex-runtime/gateway/internal/redact"
)

const (
	versionProbeTimeout = 5 * time.Second
	stderrDrainBytes    = 64 * 1024
)

type ExecutableIdentity struct {
	Path string
	info os.FileInfo
	key  string
}

type ProcessOptions struct {
	ParentEnv            map[string]string
	StderrSink           func(string)
	SchemaDiagnosticSink func(domain.GatewayErrorDetails)
	SchemaPolicy         SchemaPolicy
	ConnectionOptions    ConnectionOptions
}

func NewProcessSupervisor(ctx context.Context, cfg *config.ValidatedConfig, session config.SessionGroup, options ProcessOptions) (*Supervisor, string, error) {
	if cfg == nil {
		return nil, "", fmt.Errorf("validated config is required")
	}
	identity, err := ResolveExecutableIdentity(cfg.CodexBinary)
	if err != nil {
		return nil, "", err
	}
	parentEnv := options.ParentEnv
	if parentEnv == nil {
		parentEnv = envMapFromOS()
	}
	options.ParentEnv = parentEnv
	runtimeVersion, _ := ProbeVersion(ctx, identity, probeEnv(parentEnv))
	metadata, err := LoadVendoredSchemaMetadata()
	if err != nil {
		return nil, "", err
	}
	policy, err := NewSchemaPolicy(metadata, runtimeVersion, cfg.StrictSchemaVerificationEnabled())
	if err != nil {
		return nil, runtimeVersion, err
	}
	options.SchemaPolicy = policy
	return NewSupervisor(session.SessionGroupID, func(ctx context.Context) (*Connection, error) {
		return StartProcessConnection(ctx, cfg, session, identity, options)
	}), runtimeVersion, nil
}

func ResolveExecutableIdentity(path string) (ExecutableIdentity, error) {
	identity, err := canonicalPathIdentity(path)
	if err != nil {
		return ExecutableIdentity{}, err
	}
	if identity.info.IsDir() {
		return ExecutableIdentity{}, fmt.Errorf("executable must not be a directory")
	}
	if !identity.info.Mode().IsRegular() {
		return ExecutableIdentity{}, fmt.Errorf("executable must be a regular file")
	}
	if err := validateExecutablePermission(identity.path, identity.info); err != nil {
		return ExecutableIdentity{}, err
	}
	if err := validateExecutableTrust(identity.info); err != nil {
		return ExecutableIdentity{}, err
	}
	if err := validateExecutableParentTrust(identity.path); err != nil {
		return ExecutableIdentity{}, err
	}
	return ExecutableIdentity{
		Path: identity.path,
		info: identity.info,
		key:  identity.key,
	}, nil
}

func (i ExecutableIdentity) VerifyUnchanged() error {
	current, err := ResolveExecutableIdentity(i.Path)
	if err != nil {
		return err
	}
	if i.info != nil && current.info != nil {
		if !os.SameFile(i.info, current.info) {
			return fmt.Errorf("executable identity changed")
		}
	} else if i.key != current.key {
		return fmt.Errorf("executable identity changed")
	}
	if i.info != nil && current.info != nil && (i.info.Size() != current.info.Size() || !i.info.ModTime().Equal(current.info.ModTime())) {
		return fmt.Errorf("executable changed after startup validation")
	}
	return nil
}

func ProbeVersion(ctx context.Context, identity ExecutableIdentity, env map[string]string) (string, error) {
	if err := identity.VerifyUnchanged(); err != nil {
		return "", err
	}
	probeCtx, cancel := context.WithTimeout(ctx, versionProbeTimeout)
	defer cancel()

	command := exec.CommandContext(probeCtx, identity.Path, "--version")
	command.Env = envSlice(env)
	var stdout bytes.Buffer
	command.Stdout = limitWriter(&stdout, 4*1024)
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return "", err
	}
	return firstLine(stdout.String()), nil
}

func StartProcessConnection(ctx context.Context, cfg *config.ValidatedConfig, session config.SessionGroup, identity ExecutableIdentity, options ProcessOptions) (*Connection, error) {
	if cfg == nil {
		return nil, fmt.Errorf("validated config is required")
	}
	if err := identity.VerifyUnchanged(); err != nil {
		return nil, err
	}
	if diagnostic := options.SchemaPolicy.RuntimeDiagnostic(); diagnostic != nil {
		if cfg.StrictSchemaVerificationEnabled() {
			return nil, diagnostic
		}
		if options.SchemaDiagnosticSink != nil {
			options.SchemaDiagnosticSink(diagnostic.Details)
		}
	}

	parentEnv := options.ParentEnv
	if parentEnv == nil {
		parentEnv = envMapFromOS()
	}
	childEnv, err := cfg.BuildChildEnv(parentEnv, session)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	connectionOptions := options.ConnectionOptions
	connectionOptions.SchemaPolicy = options.SchemaPolicy
	connectionOptions.ForwardRequests = true
	if connectionOptions.SensitiveRegistry == nil {
		connectionOptions.SensitiveRegistry = redact.NewRegistry()
	}
	if session.CredentialProviderID != "" {
		var provider *config.CredentialProvider
		for index := range cfg.CredentialProviders {
			if cfg.CredentialProviders[index].ProviderID == session.CredentialProviderID {
				provider = &cfg.CredentialProviders[index]
				break
			}
		}
		if provider == nil {
			return nil, fmt.Errorf("session group %q references unknown credential provider %q", session.SessionGroupID, session.CredentialProviderID)
		}
		connectionOptions.CredentialProvider = provider
		connectionOptions.ProviderEnv = providerEnvMap(provider.EnvSources, parentEnv)
	}

	childCtx, stopChild := context.WithCancel(context.Background())
	command := exec.CommandContext(childCtx, identity.Path, "app-server", "--listen", "stdio://")
	command.Dir = session.CanonicalCWD
	command.Env = envSlice(childEnv)

	stdin, err := command.StdinPipe()
	if err != nil {
		stopChild()
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		stopChild()
		return nil, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		stopChild()
		return nil, err
	}
	if err := command.Start(); err != nil {
		stopChild()
		return nil, err
	}
	waitDone := make(chan struct{})

	redactor := connectionOptions.Redactor
	if redactor == nil {
		redactor = redact.New(redact.WithConnectionRegistry(connectionOptions.SensitiveRegistry))
	}
	go DrainStderr(childCtx, stderr, redactor, stderrDrainBytes, options.StderrSink)

	connectionOptions.OnClose = stopChild
	connectionOptions.WaitClose = func() {
		<-waitDone
	}
	connection := NewConnection(stdin, stdout, session, connectionOptions)
	go func() {
		<-connection.Done()
		stopChild()
	}()
	go func() {
		_ = command.Wait()
		stopChild()
		_ = connection.dispatcher.Close()
		close(waitDone)
	}()
	if err := connection.Initialize(ctx); err != nil {
		_ = connection.Close()
		stopChild()
		return nil, err
	}
	return connection, nil
}

func DrainStderr(ctx context.Context, reader io.Reader, redactor *redact.Redactor, maxBytes int, sink func(string)) {
	if sink == nil {
		sink = func(string) {}
	}
	stream := redact.NewStream(redactor)
	buffer := make([]byte, 4096)
	published := 0
	publish := func(chunk string) {
		if chunk == "" || maxBytes <= 0 {
			return
		}
		remaining := maxBytes - published
		if remaining <= 0 {
			return
		}
		if len(chunk) > remaining {
			chunk = chunk[:remaining]
		}
		published += len(chunk)
		if chunk != "" {
			sink(chunk)
		}
	}
	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			publish(stream.Write(string(buffer[:n])))
		}
		if err != nil {
			publish(stream.Flush())
			return
		}
		select {
		case <-ctx.Done():
			publish(stream.Flush())
			return
		default:
		}
	}
}

func envMapFromOS() map[string]string {
	env := map[string]string{}
	for _, entry := range os.Environ() {
		if key, value, ok := strings.Cut(entry, "="); ok {
			env[key] = value
		}
	}
	return env
}

func probeEnv(parent map[string]string) map[string]string {
	names := []string{"PATH", "TMPDIR", "LANG", "LC_ALL", "LC_CTYPE"}
	if runtime.GOOS == "windows" {
		names = []string{"SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT", "PATH", "TEMP", "TMP"}
	}
	env := map[string]string{}
	for _, name := range names {
		if value, ok := lookupEnv(parent, name); ok {
			env[name] = value
		}
	}
	return env
}

func envSlice(env map[string]string) []string {
	result := make([]string, 0, len(env))
	for key, value := range env {
		result = append(result, key+"="+value)
	}
	return result
}

func lookupEnv(env map[string]string, name string) (string, bool) {
	if runtime.GOOS != "windows" {
		value, ok := env[name]
		return value, ok
	}
	for key, value := range env {
		if strings.EqualFold(key, name) {
			return value, true
		}
	}
	return "", false
}

func firstLine(text string) string {
	if line, _, ok := strings.Cut(text, "\n"); ok {
		return strings.TrimSpace(line)
	}
	return strings.TrimSpace(text)
}
