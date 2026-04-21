package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// deprecationMessage is printed to stderr by the deprecation stub when a
// user invokes `vibew deploy`. The wording matches ADR-086 §"Stub command"
// verbatim and must not drift — it is asserted by
// TestDeployCmd_EmitsDeprecationAndExits2.
const deprecationMessage = `The "vibew deploy" command has been removed. Use "vibew bundle" to generate
deployment artifacts, then run the included deploy.sh manually. See
docs/deploy-reference.md for migration steps.`

// NewDeployCmd returns a hidden deprecation stub for the removed
// `vibew deploy` command.
//
// The stub ships for exactly one release and is tracked for removal by
// issue #1063 (chore: remove vibew deploy stub).
//
// Semantics (ADR-086 §"Stub command"):
//   - Hidden: true         — keeps `deploy` out of `vibew --help` but leaves
//     it dispatchable so cobra does not print `unknown command "deploy"`.
//   - DisableFlagParsing   — --target, --dry-run, and positional subcommands
//     like `status` and `logs` are swallowed; the same message fires.
//   - Run (not RunE) with direct os.Exit(2) — cmd/vibewarden/main.go maps
//     RunE errors to os.Exit(1), so a RunE path would produce exit 1 instead
//     of the documented exit 2 contract.
//
// TODO(#1063): remove this stub two releases after the sunset lands.
func NewDeployCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "deploy",
		Short:              "(removed) use vibew bundle",
		Hidden:             true,
		SilenceUsage:       true,
		SilenceErrors:      true,
		DisableFlagParsing: true,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.ErrOrStderr(), deprecationMessage)
			exitFunc(2)
		},
	}
}

// exitFunc is the process-exit hook the stub calls on every invocation. It
// is swappable in tests so unit tests can assert the exit code without
// tearing down the test binary.
var exitFunc = os.Exit
