package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/cantara/source-code-portal/internal/platform/config"
)

// loadOptions says how a command reads the configuration.
type loadOptions struct {
	file        string
	environ     []string // nil = no environment layer (lint reads the file alone)
	set         []string // --set path=value
	skipSecrets bool     // validate {file: ...} references without reading them (lint)
}

// loaded is a configuration and everything learned while reading it.
type loaded struct {
	cfg        Config
	violations []config.Violation
	warnings   []string // config.Result.Warnings plus Config.startupWarnings
}

// readConfig runs config.Load once. A *config.Error is not a Go error here: it is the list of violations, kept so that lint,
// doctor and serve can each present it their own way. Any other error (an unreadable file, a malformed document) is returned.
func readConfig(o loadOptions) (loaded, error) {
	var l loaded
	res, err := config.Load(&l.cfg, config.Options{Defaults: defaultYAML, File: o.file, ReadFile: os.ReadFile, Environ: o.environ, Set: o.set, SkipSecrets: o.skipSecrets})
	var ce *config.Error
	switch {
	case errors.As(err, &ce):
		l.violations = ce.Violations
	case err != nil:
		return l, err
	}
	l.warnings = append(l.warnings, res.Warnings...)
	if len(l.violations) == 0 {
		l.warnings = append(l.warnings, l.cfg.startupWarnings()...)
	}
	return l, nil
}

func (v loaded) violationText() string {
	var b strings.Builder
	for _, x := range v.violations {
		if x.File != "" && x.Line > 0 {
			fmt.Fprintf(&b, "%s:%d: ", x.File, x.Line)
		}
		if x.Path != "" {
			b.WriteString(x.Path + ": ")
		}
		b.WriteString(x.Message + "\n")
	}
	return b.String()
}

func splitHost(addr string) (host, port string) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, ""
	}
	return h, p
}

// isLoopbackHost: an empty host (":9091") means every interface, so it is not loopback.
func isLoopbackHost(h string) bool {
	if h == "" {
		return false
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
