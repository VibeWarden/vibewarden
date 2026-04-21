package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	yamlmodadapter "github.com/vibewarden/vibewarden/internal/adapters/yamlmod"
	scaffoldapp "github.com/vibewarden/vibewarden/internal/app/scaffold"
	"github.com/vibewarden/vibewarden/internal/cli/templates"
	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// runAddFeature executes the add-feature flow for a given feature and options.
// dir is the project directory (defaults to "." when empty).
// It is shared by all `vibew add <feature>` subcommands.
func runAddFeature(
	cmd *cobra.Command,
	dir string,
	feature domainscaffold.Feature,
	opts domainscaffold.FeatureOptions,
) error {
	if dir == "" {
		dir = "."
	}

	toggler := yamlmodadapter.NewToggler()
	renderer := templateadapter.NewRenderer(templates.FS)
	svc := scaffoldapp.NewAddFeatureService(toggler, renderer)

	addOpts := scaffoldapp.AddFeatureOptions{
		Feature:                feature,
		FeatureOptions:         opts,
		RegenerateAgentContext: true,
	}

	result, err := svc.AddFeature(context.Background(), dir, addOpts)
	if err != nil {
		if errors.Is(err, domainscaffold.ErrFeatureAlreadyEnabled) {
			if feature == domainscaffold.FeatureTLS {
				fmt.Fprintf(cmd.OutOrStdout(), "TLS is already enabled in vibewarden.yaml.\nTo update the domain, run: vibew add tls --domain <new-domain>\n")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Feature %q is already enabled in vibewarden.yaml — nothing to do.\n", feature)
			}
			return nil
		}
		if errors.Is(err, domainscaffold.ErrConfigNotFound) {
			return fmt.Errorf(
				"vibewarden.yaml not found in %q — run 'vibew wrap' first",
				dir,
			)
		}
		return err
	}

	printAddSuccessMessage(cmd, feature, result)
	PrintAddSummary(cmd.OutOrStdout(), result.ConfigDiff)
	return nil
}

// printAddSuccessMessage writes a success message and next-steps guidance.
func printAddSuccessMessage(cmd *cobra.Command, feature domainscaffold.Feature, result *scaffoldapp.AddResult) {
	w := cmd.OutOrStdout()

	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "Feature %q enabled successfully.\n", feature)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Updated:")
	fmt.Fprintf(w, "  %s\n", result.UpdatedConfig)
	for _, f := range result.RegeneratedContextFiles {
		fmt.Fprintf(w, "  %s (regenerated)\n", f)
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Review vibewarden.yaml and adjust settings as needed.")
}

// PrintAddSummary writes a human-readable per-file summary of a Diff to w.
// Called by every `vibew add <feature>` RunE after the underlying edit
// returns. It is a no-op when diff is empty so already-enabled paths do not
// print stray headers.
func PrintAddSummary(w io.Writer, diff domainscaffold.Diff) {
	if diff.IsEmpty() {
		return
	}

	fmt.Fprintf(w, "\nChanges in %s:\n", diff.File)
	for _, a := range diff.Added {
		fmt.Fprintf(w, "  + %s: %s\n", a.Path, a.After)
	}
	for _, c := range diff.Changed {
		fmt.Fprintf(w, "  ~ %s: %s -> %s\n", c.Path, c.Before, c.After)
	}
	for _, r := range diff.Removed {
		fmt.Fprintf(w, "  - %s (was: %s)\n", r.Path, r.Before)
	}
}
