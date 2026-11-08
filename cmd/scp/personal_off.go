//go:build server

package main

import (
	"errors"

	"github.com/spf13/cobra"
)

// newPersonalCmd is scp personal in the server build (go build -tags server): the personal edition is not part of this binary, so
// its command only says so. The files that hold it (personal*.go) are left out of the build, and with them the setup page, the
// sign-in to GitHub, the vault and the keychain.
func newPersonalCmd(*options) *cobra.Command {
	return &cobra.Command{
		Use:   "personal",
		Short: "Not in this build: the server build runs shared portals only (scp serve)",
		RunE: func(*cobra.Command, []string) error {
			return failure{errors.New("this is the server build, which does not include scp personal; use the full build of scp for it, or scp serve for a shared portal")}
		},
	}
}
