package config

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"

	"github.com/cantara/source-code-portal/internal/llm"
)

// LLM names the language-model provider the assistant uses (design/assistant.md); an empty provider turns the assistant off.
type LLM struct {
	Provider   string `mapstructure:"provider"`
	Model      string `mapstructure:"model"`    // empty: the provider's default
	BaseURL    string `mapstructure:"base_url"` // empty: the provider's
	APIKeyFile string `mapstructure:"api_key_file"`
	APIKey     Secret `mapstructure:"-"`
}

// Assistant configures who may ask and the budgets of one request.
type Assistant struct {
	Access          string `mapstructure:"access"` // signed-in, admin or everyone
	Concurrency     int    `mapstructure:"concurrency"`
	MaxTurns        int    `mapstructure:"max_turns"`
	MaxToolCalls    int    `mapstructure:"max_tool_calls"`
	MaxTokens       int    `mapstructure:"max_tokens"`
	MaxHistory      int    `mapstructure:"max_history"`
	MaxInputChars   int    `mapstructure:"max_input_chars"`
	ToolResultChars int    `mapstructure:"tool_result_chars"`
}

var modelPattern = regexp.MustCompile(`^[A-Za-z0-9._:/@-]{1,128}$`)

func (l LLM) validate(a Assistant, identified bool) []string {
	if l.Provider == "" {
		return nil
	}
	var p []string
	if !slices.Contains(llm.Providers(), l.Provider) {
		return []string{fmt.Sprintf("llm.provider: %q is none of %v", l.Provider, llm.Providers())}
	}
	if l.APIKeyFile == "" && !llm.Keyless(l.Provider) {
		p = append(p, "llm.api_key_file: required for "+l.Provider)
	}
	if l.Model != "" && !modelPattern.MatchString(l.Model) {
		p = append(p, fmt.Sprintf("llm.model: %q is not a model name", l.Model))
	}
	if l.BaseURL != "" {
		u, err := url.Parse(l.BaseURL)
		if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil ||
			u.Scheme != "https" && (u.Scheme != "http" || l.APIKeyFile != "" && !loopback(u.Hostname())) {
			p = append(p, fmt.Sprintf("llm.base_url: %q is not an https URL (http only on loopback or without a key)", l.BaseURL))
		}
	}
	if !slices.Contains([]string{"signed-in", "admin", "everyone"}, a.Access) {
		p = append(p, fmt.Sprintf("assistant.access: %q is none of signed-in, admin, everyone", a.Access))
	} else if a.Access != "everyone" && !identified {
		p = append(p, "assistant.access: "+a.Access+" needs a way to know the viewer: server.trusted_proxies, auth.oidc or auth.tokens")
	}
	for _, r := range []struct {
		key       string
		v, lo, hi int
	}{
		{"assistant.concurrency", a.Concurrency, 1, 64}, {"assistant.max_turns", a.MaxTurns, 1, 32},
		{"assistant.max_tool_calls", a.MaxToolCalls, 0, 64}, {"assistant.max_tokens", a.MaxTokens, 256, 200_000},
		{"assistant.max_history", a.MaxHistory, 1, 200}, {"assistant.max_input_chars", a.MaxInputChars, 1000, 1_000_000},
		{"assistant.tool_result_chars", a.ToolResultChars, 1000, 200_000},
	} {
		if r.v < r.lo || r.v > r.hi {
			p = append(p, fmt.Sprintf("%s: must be %d..%d", r.key, r.lo, r.hi))
		}
	}
	return p
}

func (l *LLM) readSecrets(read func(key, path string, dst *Secret)) {
	if l.Provider != "" && l.APIKeyFile != "" {
		read("llm.api_key_file", l.APIKeyFile, &l.APIKey)
	}
}
