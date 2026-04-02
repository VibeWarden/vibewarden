package cmd

import (
	"context"
	"errors"
	"fmt"

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
		Feature:        feature,
		FeatureOptions: opts,
		AgentType:      domainscaffold.AgentTypeAll,
	}

	result, err := svc.AddFeature(context.Background(), dir, addOpts)
	if err != nil {
		if errors.Is(err, domainscaffold.ErrFeatureAlreadyEnabled) {
			fmt.Fprintf(cmd.OutOrStdout(), "Feature %q is already enabled in vibewarden.yaml — nothing to do.\n", feature)
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
