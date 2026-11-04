package main

import (
	"context"
	"log/slog"

	"github.com/cantara/source-code-portal/internal/assistant"
	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/llm"
	"github.com/cantara/source-code-portal/internal/tools"
)

// newAssistant wires the assistant from the config; without a provider it is off and answers only its status.
func newAssistant(cfg config.Config, catalogue *tools.Catalogue, groups func(context.Context) []string, observe func(string, int, int),
	log *slog.Logger,
) (*assistant.Assistant, error) {
	o := assistant.Options{Access: cfg.Assistant.Access, PublicURL: cfg.Server.PublicURL, Groups: groups, Observe: observe, Log: log}
	if cfg.LLM.Provider == "" {
		return assistant.New(o), nil
	}
	chat, err := llm.New(llm.Config{Provider: cfg.LLM.Provider, Model: cfg.LLM.Model, BaseURL: cfg.LLM.BaseURL, APIKey: cfg.LLM.APIKey.Reveal()})
	if err != nil {
		return nil, err
	}
	a := cfg.Assistant
	o.Chat, o.Tools = chat, catalogue
	o.Budget = assistant.Budget{Concurrency: a.Concurrency, MaxTurns: a.MaxTurns, MaxToolCalls: a.MaxToolCalls, MaxTokens: a.MaxTokens,
		MaxHistory: a.MaxHistory, MaxInputChars: a.MaxInputChars, ToolResultChars: a.ToolResultChars}
	return assistant.New(o), nil
}
