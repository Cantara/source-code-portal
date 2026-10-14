package main

import (
	"github.com/cantara/source-code-portal/internal/platform/httpx"
	"github.com/cantara/source-code-portal/internal/platform/lifecycle"
	"github.com/cantara/source-code-portal/internal/platform/logx"
	"github.com/cantara/source-code-portal/internal/platform/telemetry"
)

// Config is the root configuration struct (composition only; keys live in the feature-owned types).
type Config struct {
	Version   int              `yaml:"version" jsonschema:"required,enum=1" jsonschema_description:"Config schema version. Must be 1."`
	Server    httpx.Config     `yaml:"server" jsonschema_description:"HTTP listeners."`
	Log       logx.Config      `yaml:"log" jsonschema_description:"Logging."`
	Telemetry telemetry.Config `yaml:"telemetry" jsonschema_description:"Metrics, traces and OTLP export."`
	RateLimit httpx.RateConfig `yaml:"ratelimit" jsonschema_description:"Keyed rate limiter."`
	Shutdown  lifecycle.Config `yaml:"shutdown" jsonschema_description:"Graceful shutdown budget."`
}
