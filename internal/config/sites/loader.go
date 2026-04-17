package sites

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/domain/site"
)

// LoadSites scans basePath for subdirectories, each containing a
// vibewarden.yaml. For each subdirectory it loads the config via the
// existing config.Load function and constructs a Site.
//
// Partial success: if one site fails to load, the error is captured in
// an error Site and returned alongside the healthy ones. The caller
// receives all successfully-loaded sites plus a slice of per-site
// errors so that a broken app2 config does not prevent app1 from
// serving.
//
// If basePath does not exist, LoadSites returns (nil, nil) — this is
// the backward-compatible single-app mode where no sites/ directory
// is present.
func LoadSites(basePath string) ([]*site.Site, []error) {
	entries, err := os.ReadDir(basePath)
	if err != nil {
		if os.IsNotExist(err) {
			// No sites/ directory — single-app mode.
			return nil, nil
		}
		return nil, []error{fmt.Errorf("reading sites directory %s: %w", basePath, err)}
	}

	var (
		sites []*site.Site
		errs  []error
	)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		configPath := filepath.Join(basePath, name, "vibewarden.yaml")

		// Check the config file exists before attempting Load.
		if _, statErr := os.Stat(configPath); statErr != nil {
			if os.IsNotExist(statErr) {
				// Subdirectory without a vibewarden.yaml — skip silently.
				continue
			}
			loadErr := fmt.Errorf("checking site %q config: %w", name, statErr)
			errs = append(errs, loadErr)
			errSite, siteErr := site.NewErrorSite(name, configPath, loadErr)
			if siteErr != nil {
				// Name itself is invalid — record the error but skip the site.
				errs = append(errs, fmt.Errorf("invalid site directory name %q: %w", name, siteErr))
				continue
			}
			sites = append(sites, errSite)
			continue
		}

		cfg, loadErr := config.Load(configPath)
		if loadErr != nil {
			wrappedErr := fmt.Errorf("loading site %q config: %w", name, loadErr)
			errs = append(errs, wrappedErr)
			errSite, siteErr := site.NewErrorSite(name, configPath, wrappedErr)
			if siteErr != nil {
				// Name is not DNS-safe — record error but skip the site entity.
				errs = append(errs, fmt.Errorf("invalid site directory name %q: %w", name, siteErr))
				continue
			}
			sites = append(sites, errSite)
			continue
		}

		s, siteErr := site.NewSite(name, configPath, cfg)
		if siteErr != nil {
			wrappedErr := fmt.Errorf("creating site %q: %w", name, siteErr)
			errs = append(errs, wrappedErr)
			continue
		}
		sites = append(sites, s)
	}

	return sites, errs
}
