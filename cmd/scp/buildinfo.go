package main

import (
	"runtime"
	"runtime/debug"
)

// version and build are stamped by `-ldflags "-X main.version=... -X main.build=..."` (Makefile, Dockerfile, GoReleaser);
// -X can set only package-level variables, so .golangci.yml exempts this file from gochecknoglobals.
var (
	version = "dev" // release tag without the leading v, or "dev"
	build   = ""    // git commit SHA of the build
)

// BuildInfo is the output of `scp version --json`.
type BuildInfo struct {
	Version  string `json:"version"`
	Build    string `json:"build"`
	Modified bool   `json:"modified"` // vcs.modified from the Go toolchain; false when the build had no VCS stamp
	Go       string `json:"go"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
}

func readBuildInfo() BuildInfo {
	bi := BuildInfo{Version: version, Build: build, Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if bi.Build == "" {
					bi.Build = s.Value
				}
			case "vcs.modified":
				bi.Modified = s.Value == "true"
			}
		}
	}
	if bi.Build == "" {
		bi.Build = "unknown"
	}
	return bi
}
