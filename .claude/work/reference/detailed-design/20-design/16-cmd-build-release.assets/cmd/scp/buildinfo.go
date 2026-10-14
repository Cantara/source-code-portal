package main

import (
	"runtime"
	"runtime/debug"

	"github.com/cantara/source-code-portal/web"
)

// version and build are stamped by `-ldflags "-X main.version=... -X main.build=..."` (Makefile, Dockerfile, GoReleaser).
// They are the only package-level variables of the module besides sentinel errors, because -X cannot set anything else;
// .golangci.yml exempts exactly this file from gochecknoglobals. Nothing assigns them at run time.
var (
	version = "dev" // release tag without the leading v, or "dev"
	build   = ""    // git commit SHA of the build; also the SPA build id (X-SCP-Build) and the Vite VITE_BUILD_ID
)

// BuildInfo is the output of `scp version --json` and the `build` metric labels.
type BuildInfo struct {
	Version  string `json:"version"`
	Build    string `json:"build"`
	Modified bool   `json:"modified"` // vcs.modified from the Go toolchain; false when the build had no VCS stamp
	Go       string `json:"go"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	SPA      string `json:"spa"` // "spa" or "placeholder": which bundle is embedded
}

func readBuildInfo() BuildInfo {
	bi := BuildInfo{Version: version, Build: build, Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, SPA: string(web.EmbeddedMode)}
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
