package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	lares "github.com/riko/lares"
	"github.com/riko/lares/internal/coi"
)

// The sandbox keeps its secrets in one file, next to the coi profile. pi never
// reads that file directly: it calls lares-token, which prints one value, so the
// tokens do not sit in the environment of every process in the container.
const (
	secretsFileName      = "secrets.env"
	containerSecretsPath = "/home/code/.config/lares/secrets"
	containerTokenHelper = "/usr/local/bin/lares-token"

	// obsoleteMCPMountPath is where older profiles mounted the host
	// mcp-adapter.json. lares now pushes the rendered file into the agent
	// directory instead.
	obsoleteMCPMountPath = "/opt/pi/mcp-adapter.json"
)

// secretsTemplate is written next to the profile the first time lares installs
// it. The values are empty on purpose: the sandbox credentials are not the
// host ones, and the user has to provide them.
const secretsTemplate = `# Secrets for the lares sandbox.
#
# lares copies this file into the container and lares-token reads from it when
# pi needs a credential. Use credentials dedicated to the sandbox, not the host
# ones.
#
# lares-token takes the variable name directly: lares-token CONTEXT7_KEY.
# For an api_key provider, pi's auth.json reads the provider name uppercased,
# with '-' replaced by '_' (opencode-go -> OPENCODE_GO).
#
# Keep this file private: chmod 600.

# Context7 (MCP documentation)
CONTEXT7_KEY=

# Basic Memory (MCP knowledge base)
BM_TOKEN=

# Tavily (web search)
TAVILY=

# OpenCode Go (pi's default provider)
OPENCODE_GO=
`

// providerEnvName is the secrets variable for an api_key provider: the provider
// name uppercased, with '-' replaced by '_'. opencode-go therefore reads
// OPENCODE_GO from the secrets file. There is no table to keep in sync: the
// name is derived from the provider.
func providerEnvName(provider string) string {
	return strings.ToUpper(strings.ReplaceAll(provider, "-", "_"))
}

// syncSandboxSecrets copies the sandbox secrets file and the helper that reads
// it. Every tool gets them, so a shell can use lares-token too.
func syncSandboxSecrets(name string) {
	syncSecrets(name)
	pushTokenHelper(name)
}

// syncPiCredentials renders the auth/MCP/web-search configurations pi reads.
// They call lares-token, which syncSandboxSecrets installs.
func syncPiCredentials(name string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	removeObsoleteMCPMount(name, home)

	syncRendered(name,
		filepath.Join(home, piAgentDir, "auth.json"),
		path.Join(containerHome, piAgentDir, "auth.json"),
		renderAuth)

	syncRendered(name,
		filepath.Join(home, piAgentDir, "mcp-adapter.json"),
		path.Join(containerHome, piAgentDir, "mcp-adapter.json"),
		renderMCPConfig)

	// web-search.json has no useful host counterpart to preserve: the sandbox
	// version only needs to declare where the Tavily key comes from.
	data, err := os.ReadFile(filepath.Join(home, piAgentDir, "web-search.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "lares: reading web-search.json: %v\n", err)
		return
	}

	renderAndPush(name, path.Join(containerHome, piAgentDir, "web-search.json"), data, renderWebSearch)
}

// installSecrets creates the sandbox secrets file from the template when it is
// missing, and reports whether it created one. An existing file is never
// rewritten: it holds the credentials the sandbox uses.
func installSecrets(dir string) (bool, error) {
	path := filepath.Join(dir, secretsFileName)

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	if _, err := file.WriteString(secretsTemplate); err != nil {
		_ = file.Close()

		return false, err
	}

	if err := file.Close(); err != nil {
		return false, err
	}

	return true, nil
}

// syncSecrets copies the sandbox secrets file into the container, readable by
// the container user only. Anything in the file is therefore visible to the
// sandbox: it is the one file that holds the credentials the sandbox may use.
func syncSecrets(name string) {
	dir, err := profileDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "lares: locating the sandbox profile: %v\n", err)

		return
	}

	local := filepath.Join(dir, secretsFileName)

	info, err := os.Stat(local)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "lares: no secrets file at %s; run `lares install` to create it\n", local)
		} else {
			fmt.Fprintf(os.Stderr, "lares: reading %s: %v\n", local, err)
		}

		return
	}

	if info.Mode().Perm()&0o077 != 0 {
		fmt.Fprintf(os.Stderr, "lares: %s is readable by other users; run chmod 600 on it\n", local)
	}

	if body, err := os.ReadFile(local); err == nil && string(body) == secretsTemplate {
		fmt.Fprintf(os.Stderr, "lares: %s has no credentials yet; fill it before using pi or lares-token\n", local)
	}

	containerDir := path.Dir(containerSecretsPath)
	if err := coi.Exec(name, "install", "-d", "-m", "0700", "-o", "code", "-g", "code", containerDir); err != nil {
		fmt.Fprintf(os.Stderr, "lares: preparing %s in the sandbox: %v\n", containerDir, err)

		return
	}

	if err := coi.PushFile(local, name+":"+containerSecretsPath); err != nil {
		fmt.Fprintf(os.Stderr, "lares: copying the secrets into the sandbox: %v\n", err)

		return
	}

	if err := coi.Exec(name, "chown", "code:code", containerSecretsPath); err != nil {
		fmt.Fprintf(os.Stderr, "lares: setting the owner of the secrets file: %v\n", err)
	}

	if err := coi.Exec(name, "chmod", "0600", containerSecretsPath); err != nil {
		fmt.Fprintf(os.Stderr, "lares: setting the mode of the secrets file: %v\n", err)
	}
}

// removeObsoleteMCPMount drops the read-only mount of the host
// mcp-adapter.json that older profiles declared at /opt. lares now pushes the
// rendered file into the agent directory, where the adapter reads it, so the
// mount is dead weight and one copy of a host file the sandbox does not need.
func removeObsoleteMCPMount(name, home string) {
	source := filepath.Join(home, piAgentDir, "mcp-adapter.json")

	device, mounted, err := coi.MountDevice(name, source)
	if err != nil || !mounted {
		return
	}

	if err := coi.Unmount(name, device); err != nil {
		fmt.Fprintf(os.Stderr, "lares: removing the old mcp-adapter.json mount: %v\n", err)

		return
	}

	// Unmounting leaves the empty mount point behind.
	if err := coi.Exec(name, "rm", "-f", obsoleteMCPMountPath); err != nil {
		fmt.Fprintf(os.Stderr, "lares: removing %s: %v\n", obsoleteMCPMountPath, err)
	}
}

// pushTokenHelper installs lares-token in the container. It is pushed rather
// than baked into the image so an existing sandbox picks it up without a
// rebuild.
func pushTokenHelper(name string) {
	if err := pushData(name, containerTokenHelper, lares.TokenHelper, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "lares: installing %s in the sandbox: %v\n", containerTokenHelper, err)
	}
}

// syncRendered reads a host pi file, renders the sandbox version, and pushes
// it. A missing host file leaves the container file alone.
func syncRendered(name, local, remote string, render func([]byte) ([]byte, error)) {
	data, err := os.ReadFile(local)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "lares: reading %s: %v\n", local, err)
		}

		return
	}

	renderAndPush(name, remote, data, render)
}

func renderAndPush(name, remote string, data []byte, render func([]byte) ([]byte, error)) {
	rendered, err := render(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lares: rendering %s: %v\n", path.Base(remote), err)

		return
	}

	if err := pushData(name, remote, rendered, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "lares: writing %s in the sandbox: %v\n", remote, err)
	}
}

// pushData writes data to remote in the container with the given mode. The
// destination is removed first because an older sandbox may have a symlink
// there, pointing at the read-only mount the profile used to declare.
func pushData(name, remote string, data []byte, mode os.FileMode) error {
	file, err := os.CreateTemp("", "lares-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())

	if _, err := file.Write(data); err != nil {
		_ = file.Close()

		return err
	}

	if err := file.Close(); err != nil {
		return err
	}

	if err := os.Chmod(file.Name(), mode); err != nil {
		return err
	}

	if err := coi.Exec(name, "rm", "-f", remote); err != nil {
		return err
	}

	if err := coi.PushFile(file.Name(), name+":"+remote); err != nil {
		return err
	}

	if err := coi.Exec(name, "chown", "code:code", remote); err != nil {
		return err
	}

	return coi.Exec(name, "chmod", fmt.Sprintf("%04o", mode.Perm()), remote)
}

// renderAuth rewrites every api_key provider so pi asks lares-token for the
// value instead of reading a copied key. OAuth entries are left alone.
func renderAuth(data []byte) ([]byte, error) {
	var providers map[string]json.RawMessage
	if err := json.Unmarshal(data, &providers); err != nil {
		return nil, err
	}

	for provider, raw := range providers {
		var entry map[string]any
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, fmt.Errorf("%s: %w", provider, err)
		}

		if entry["type"] != "api_key" {
			continue
		}

		entry["key"] = tokenCommand(providerEnvName(provider))

		encoded, err := json.Marshal(entry)
		if err != nil {
			return nil, err
		}

		providers[provider] = encoded
	}

	return marshalConfig(providers)
}

// renderMCPConfig turns "Authorization: Bearer ${CONTEXT7_KEY}" into the
// bearerToken command the MCP adapter resolves when it connects. The variable
// name comes straight from the header.
func renderMCPConfig(data []byte) ([]byte, error) {
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	servers, _ := config["mcpServers"].(map[string]any)

	for _, raw := range servers {
		server, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		headers, ok := server["headers"].(map[string]any)
		if !ok {
			continue
		}

		authorization, ok := headers["Authorization"].(string)
		if !ok {
			continue
		}

		variable, ok := bearerVariable(authorization)
		if !ok {
			continue
		}

		server["auth"] = "bearer"
		server["bearerToken"] = tokenCommand(variable)

		delete(headers, "Authorization")

		if len(headers) == 0 {
			delete(server, "headers")
		}
	}

	return marshalConfig(config)
}

// bearerVariable returns the variable named by an Authorization header of the
// form "Bearer ${CONTEXT7_KEY}".
func bearerVariable(header string) (string, bool) {
	const prefix = "Bearer "

	if !strings.HasPrefix(header, prefix) {
		return "", false
	}

	value := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if !strings.HasPrefix(value, "${") || !strings.HasSuffix(value, "}") {
		return "", false
	}

	name := strings.TrimSuffix(strings.TrimPrefix(value, "${"), "}")

	return name, name != ""
}

// renderWebSearch points the Tavily search at lares-token, keeping the other
// settings of the host file when there is one.
func renderWebSearch(data []byte) ([]byte, error) {
	config := map[string]any{}

	if len(data) > 0 {
		if err := json.Unmarshal(data, &config); err != nil {
			return nil, err
		}
	}

	config["tavilyApiKey"] = tokenCommand("TAVILY")

	return marshalConfig(config)
}

// tokenCommand is the `!command` form pi and the MCP adapter run when they need
// a credential.
func tokenCommand(name string) string {
	return "!" + containerTokenHelper + " " + name
}

func marshalConfig(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}

	return append(data, '\n'), nil
}
