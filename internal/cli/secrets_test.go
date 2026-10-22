package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	lares "github.com/riko/lares"
)

func decode(t *testing.T, data []byte, value any) {
	t.Helper()

	if err := json.Unmarshal(data, value); err != nil {
		t.Fatalf("decoding the rendered configuration: %v\n%s", err, data)
	}
}

func TestRenderAuthUsesTokenCommandForAPIKeys(t *testing.T) {
	input := `{
		"opencode-go": {"type": "api_key", "key": "a-secret"},
		"other": {"type": "api_key", "key": "kept"}
	}`

	rendered, err := renderAuth([]byte(input))
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]map[string]any
	decode(t, rendered, &got)

	if got["opencode-go"]["key"] != "!/usr/local/bin/lares-token OPENCODE_GO" {
		t.Errorf("opencode-go key = %v, want the lares-token command", got["opencode-go"]["key"])
	}

	if got["opencode-go"]["type"] != "api_key" {
		t.Errorf("opencode-go type = %v, want api_key", got["opencode-go"]["type"])
	}

	// Every api_key provider is resolved from its uppercased name.
	if got["other"]["key"] != "!/usr/local/bin/lares-token OTHER" {
		t.Errorf("other key = %v, want the lares-token command", got["other"]["key"])
	}
}

func TestRenderAuthLeavesOAuthAlone(t *testing.T) {
	input := `{"opencode-go": {"type": "oauth", "key": "an-oauth-token"}}`

	rendered, err := renderAuth([]byte(input))
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]map[string]any
	decode(t, rendered, &got)

	if got["opencode-go"]["key"] != "an-oauth-token" {
		t.Errorf("opencode-go key = %v, want the OAuth credential left alone", got["opencode-go"]["key"])
	}
}

func TestRenderMCPConfigUsesBearerTokenCommand(t *testing.T) {
	input := `{
		"mcpServers": {
			"context7": {
				"url": "https://mcp.context7.com/mcp",
				"headers": {"Authorization": "Bearer ${CONTEXT7_KEY}"},
				"directTools": true
			},
			"basic-memory": {
				"url": "https://bm.ebag.click/mcp",
				"headers": {"Authorization": "Bearer ${BM_TOKEN}"}
			}
		}
	}`

	rendered, err := renderMCPConfig([]byte(input))
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Servers map[string]struct {
			Auth        string         `json:"auth"`
			BearerToken string         `json:"bearerToken"`
			Headers     map[string]any `json:"headers"`
			DirectTools bool           `json:"directTools"`
		} `json:"mcpServers"`
	}
	decode(t, rendered, &got)

	context7 := got.Servers["context7"]
	if context7.Auth != "bearer" || context7.BearerToken != "!/usr/local/bin/lares-token CONTEXT7_KEY" {
		t.Errorf("context7 auth = %q/%q, want bearer via lares-token", context7.Auth, context7.BearerToken)
	}

	if _, ok := context7.Headers["Authorization"]; ok {
		t.Error("context7 kept the Authorization header")
	}

	if !context7.DirectTools {
		t.Error("context7 lost directTools")
	}

	memory := got.Servers["basic-memory"]
	if memory.BearerToken != "!/usr/local/bin/lares-token BM_TOKEN" {
		t.Errorf("basic-memory bearerToken = %q", memory.BearerToken)
	}
}

func TestRenderMCPConfigKeepsOtherHeaders(t *testing.T) {
	input := `{"mcpServers": {"context7": {"headers": {
		"Authorization": "Bearer ${CONTEXT7_KEY}",
		"X-Other": "kept"
	}}}}`

	rendered, err := renderMCPConfig([]byte(input))
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Servers map[string]struct {
			Headers map[string]any `json:"headers"`
		} `json:"mcpServers"`
	}
	decode(t, rendered, &got)

	headers := got.Servers["context7"].Headers
	if headers["X-Other"] != "kept" {
		t.Errorf("X-Other = %v, want it kept", headers["X-Other"])
	}

	if _, ok := headers["Authorization"]; ok {
		t.Error("the Authorization header was not removed")
	}
}

func TestRenderMCPConfigConvertsAnyVariable(t *testing.T) {
	input := `{"mcpServers": {"other": {"headers": {"Authorization": "Bearer ${OTHER_TOKEN}"}}}}`

	rendered, err := renderMCPConfig([]byte(input))
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Servers map[string]struct {
			Auth        string         `json:"auth"`
			BearerToken string         `json:"bearerToken"`
			Headers     map[string]any `json:"headers"`
		} `json:"mcpServers"`
	}
	decode(t, rendered, &got)

	server := got.Servers["other"]
	if server.Auth != "bearer" || server.BearerToken != "!/usr/local/bin/lares-token OTHER_TOKEN" {
		t.Errorf("other auth = %q/%q, want bearer via lares-token", server.Auth, server.BearerToken)
	}

	if _, ok := server.Headers["Authorization"]; ok {
		t.Error("the Authorization header was not removed")
	}
}

func TestRenderWebSearchUsesTavilyCommandAndKeepsSettings(t *testing.T) {
	input := `{"tavilyApiKey": "a-literal-key", "tavilyBaseUrl": "https://example.test"}`

	rendered, err := renderWebSearch([]byte(input))
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	decode(t, rendered, &got)

	if got["tavilyApiKey"] != "!/usr/local/bin/lares-token TAVILY" {
		t.Errorf("tavilyApiKey = %v, want the lares-token command", got["tavilyApiKey"])
	}

	if got["tavilyBaseUrl"] != "https://example.test" {
		t.Errorf("tavilyBaseUrl = %v, want it kept", got["tavilyBaseUrl"])
	}
}

func TestRenderWebSearchWithoutAHostFile(t *testing.T) {
	rendered, err := renderWebSearch(nil)
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	decode(t, rendered, &got)

	if got["tavilyApiKey"] != "!/usr/local/bin/lares-token TAVILY" {
		t.Errorf("tavilyApiKey = %v, want the lares-token command", got["tavilyApiKey"])
	}
}

func TestInstallSecretsCreatesTheTemplateOnce(t *testing.T) {
	dir := t.TempDir()

	created, err := installSecrets(dir)
	if err != nil {
		t.Fatal(err)
	}

	if !created {
		t.Fatal("the secrets file was not created")
	}

	path := filepath.Join(dir, secretsFileName)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("secrets file mode = %o, want 600", mode)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(body), "CONTEXT7_KEY=") {
		t.Error("the template does not declare CONTEXT7_KEY")
	}

	if err := os.WriteFile(path, []byte("CONTEXT7_KEY=sandbox-only\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	created, err = installSecrets(dir)
	if err != nil {
		t.Fatal(err)
	}

	if created {
		t.Error("an existing secrets file was reported as created")
	}

	body, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(body) != "CONTEXT7_KEY=sandbox-only\n" {
		t.Errorf("the secrets file was rewritten: %q", body)
	}
}

func TestTokenHelperReadsTheSecretsFile(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is not available")
	}

	secrets := filepath.Join(t.TempDir(), "secrets")

	body := "CONTEXT7_KEY=context7-value\nBM_TOKEN=memory-value\nTAVILY=tavily-value\nOPENCODE_GO=opencode-value\n"
	if err := os.WriteFile(secrets, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	helper := filepath.Join(t.TempDir(), "lares-token")
	if err := os.WriteFile(helper, lares.TokenHelper, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"CONTEXT7_KEY": "context7-value",
		"BM_TOKEN":     "memory-value",
		"TAVILY":       "tavily-value",
		"OPENCODE_GO":  "opencode-value",
	}

	for name, want := range cases {
		command := exec.Command(helper, name)
		command.Env = append(os.Environ(), "LARES_SECRETS="+secrets)

		got, err := command.Output()
		if err != nil {
			t.Errorf("lares-token %s: %v", name, err)

			continue
		}

		if string(got) != want {
			t.Errorf("lares-token %s = %q, want %q", name, got, want)
		}
	}

	for _, name := range []string{"MISSING_KEY", "BAD-NAME"} {
		command := exec.Command(helper, name)
		command.Env = append(os.Environ(), "LARES_SECRETS="+secrets)

		if err := command.Run(); err == nil {
			t.Errorf("lares-token %s succeeded, want an error", name)
		}
	}
}
