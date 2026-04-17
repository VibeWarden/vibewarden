package reload

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/domain/site"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ApplyFunc is called after registry mutations to rebuild the proxy config
// from the current set of healthy sites. It receives the current context and
// returns an error if the proxy reload fails.
type ApplyFunc func(ctx context.Context) error

// MultiSiteService consumes SiteEvent values from a SiteWatcher and maps them
// to Registry mutations (Add, Remove, SetErr). After each mutation it calls
// the ApplyFunc to apply the new state to the running proxy.
//
// Error isolation: a broken site's configuration never prevents healthy sites
// from serving. If the proxy reload fails after a mutation, the service
// performs a rollback (undo the registry change and re-reload).
type MultiSiteService struct {
	registry *site.Registry
	eventLog ports.EventLogger
	logger   *slog.Logger
	reloadFn ApplyFunc
}

// NewMultiSiteService creates a MultiSiteService.
//
// registry is the shared site registry that holds the current set of sites.
// eventLog emits structured domain events; may be nil (events are skipped).
// reloadFn is called after each registry mutation to apply changes to the proxy.
func NewMultiSiteService(
	registry *site.Registry,
	eventLog ports.EventLogger,
	logger *slog.Logger,
	reloadFn ApplyFunc,
) *MultiSiteService {
	return &MultiSiteService{
		registry: registry,
		eventLog: eventLog,
		logger:   logger,
		reloadFn: reloadFn,
	}
}

// Run consumes events from the SiteWatcher channel until it is closed or ctx
// is cancelled. Each event is handled in sequence. Run blocks until the
// channel is closed.
func (ms *MultiSiteService) Run(ctx context.Context, events <-chan ports.SiteEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			ms.handleEvent(ctx, ev)
		}
	}
}

// handleEvent dispatches a single SiteEvent to the appropriate handler.
func (ms *MultiSiteService) handleEvent(ctx context.Context, ev ports.SiteEvent) {
	switch ev.Kind {
	case ports.SiteEventCreated:
		ms.handleCreated(ctx, ev)
	case ports.SiteEventModified:
		ms.handleModified(ctx, ev)
	case ports.SiteEventRemoved:
		ms.handleRemoved(ctx, ev)
	default:
		ms.logger.Warn("unknown site event kind",
			slog.String("site", ev.SiteName),
			slog.Int("kind", int(ev.Kind)),
		)
	}
}

// handleCreated loads a new site config, adds it to the registry, validates
// domains, and triggers a proxy reload.
func (ms *MultiSiteService) handleCreated(ctx context.Context, ev ports.SiteEvent) {
	ms.logger.Info("site created", slog.String("site", ev.SiteName), slog.String("config", ev.ConfigPath))

	cfg, err := config.Load(ev.ConfigPath)
	if err != nil {
		ms.logger.Error("failed to load new site config",
			slog.String("site", ev.SiteName),
			slog.String("error", err.Error()),
		)
		ms.markSiteError(ev, fmt.Errorf("loading config: %w", err))
		ms.emitSiteLoadFailed(ctx, ev, err.Error())
		return
	}

	s, err := site.NewSite(ev.SiteName, ev.ConfigPath, cfg)
	if err != nil {
		ms.logger.Error("failed to create site entity",
			slog.String("site", ev.SiteName),
			slog.String("error", err.Error()),
		)
		ms.emitSiteLoadFailed(ctx, ev, err.Error())
		return
	}

	ms.registry.Add(s)

	// Validate that the new site does not introduce domain conflicts.
	if domErr := ms.registry.ValidateDomains(); domErr != nil {
		ms.logger.Error("domain validation failed after adding site",
			slog.String("site", ev.SiteName),
			slog.String("error", domErr.Error()),
		)
		// Roll back: remove the conflicting site.
		ms.registry.Remove(ev.SiteName)
		ms.emitSiteLoadFailed(ctx, ev, domErr.Error())
		return
	}

	if err := ms.reloadFn(ctx); err != nil {
		ms.logger.Error("proxy reload failed after site add",
			slog.String("site", ev.SiteName),
			slog.String("error", err.Error()),
		)
		// Roll back: remove the site that caused the failure.
		ms.registry.Remove(ev.SiteName)
		ms.emitSiteLoadFailed(ctx, ev, "proxy reload failed: "+err.Error())
		return
	}

	domain := ""
	if cfg.TLS.Domain != "" {
		domain = cfg.TLS.Domain
	}
	ms.emitSiteAdded(ctx, ev, domain)
}

// handleModified reloads an existing site's config, updates the registry,
// and triggers a proxy reload.
func (ms *MultiSiteService) handleModified(ctx context.Context, ev ports.SiteEvent) {
	ms.logger.Info("site modified", slog.String("site", ev.SiteName), slog.String("config", ev.ConfigPath))

	cfg, err := config.Load(ev.ConfigPath)
	if err != nil {
		ms.logger.Error("failed to reload site config",
			slog.String("site", ev.SiteName),
			slog.String("error", err.Error()),
		)
		ms.markSiteError(ev, fmt.Errorf("loading config: %w", err))
		ms.emitSiteLoadFailed(ctx, ev, err.Error())
		return
	}

	newSite, err := site.NewSite(ev.SiteName, ev.ConfigPath, cfg)
	if err != nil {
		ms.logger.Error("failed to create updated site entity",
			slog.String("site", ev.SiteName),
			slog.String("error", err.Error()),
		)
		ms.emitSiteLoadFailed(ctx, ev, err.Error())
		return
	}

	// Snapshot the previous site for rollback.
	prevSite, hadPrevious := ms.registry.Get(ev.SiteName)

	ms.registry.Add(newSite)

	// Validate domains after update.
	if domErr := ms.registry.ValidateDomains(); domErr != nil {
		ms.logger.Error("domain validation failed after updating site",
			slog.String("site", ev.SiteName),
			slog.String("error", domErr.Error()),
		)
		ms.rollbackSite(prevSite, hadPrevious, ev.SiteName)
		ms.emitSiteLoadFailed(ctx, ev, domErr.Error())
		return
	}

	if err := ms.reloadFn(ctx); err != nil {
		ms.logger.Error("proxy reload failed after site update",
			slog.String("site", ev.SiteName),
			slog.String("error", err.Error()),
		)
		ms.rollbackSite(prevSite, hadPrevious, ev.SiteName)
		ms.emitSiteLoadFailed(ctx, ev, "proxy reload failed: "+err.Error())
		return
	}

	domain := ""
	if cfg.TLS.Domain != "" {
		domain = cfg.TLS.Domain
	}
	ms.emitSiteUpdated(ctx, ev, domain)
}

// handleRemoved removes a site from the registry and triggers a proxy reload.
func (ms *MultiSiteService) handleRemoved(ctx context.Context, ev ports.SiteEvent) {
	ms.logger.Info("site removed", slog.String("site", ev.SiteName), slog.String("config", ev.ConfigPath))

	// Snapshot for rollback.
	prevSite, hadPrevious := ms.registry.Get(ev.SiteName)

	existed := ms.registry.Remove(ev.SiteName)
	if !existed {
		ms.logger.Debug("removed site was not in registry",
			slog.String("site", ev.SiteName),
		)
		return
	}

	if err := ms.reloadFn(ctx); err != nil {
		ms.logger.Error("proxy reload failed after site removal",
			slog.String("site", ev.SiteName),
			slog.String("error", err.Error()),
		)
		// Roll back: re-add the site.
		ms.rollbackSite(prevSite, hadPrevious, ev.SiteName)
		return
	}

	ms.emitSiteRemoved(ctx, ev)
}

// markSiteError records an error site in the registry without triggering a
// proxy reload. This ensures the site appears in status output as errored.
func (ms *MultiSiteService) markSiteError(ev ports.SiteEvent, siteErr error) {
	errSite, err := site.NewErrorSite(ev.SiteName, ev.ConfigPath, siteErr)
	if err != nil {
		ms.logger.Error("failed to create error site entity",
			slog.String("site", ev.SiteName),
			slog.String("error", err.Error()),
		)
		return
	}
	ms.registry.Add(errSite)
}

// rollbackSite restores a previous site in the registry, or removes the
// site if there was no previous version.
func (ms *MultiSiteService) rollbackSite(prev *site.Site, hadPrevious bool, name string) {
	if hadPrevious && prev != nil {
		ms.registry.Add(prev)
	} else {
		ms.registry.Remove(name)
	}
}

// ------------------------------------------------------------------
// Domain event emission helpers
// ------------------------------------------------------------------

func (ms *MultiSiteService) emitSiteAdded(ctx context.Context, ev ports.SiteEvent, domain string) {
	if ms.eventLog == nil {
		return
	}
	e := events.NewSiteAdded(events.SiteAddedParams{
		SiteName:   ev.SiteName,
		ConfigPath: ev.ConfigPath,
		Domain:     domain,
	})
	if err := ms.eventLog.Log(ctx, e); err != nil {
		ms.logger.Error("failed to emit site.added event", slog.String("error", err.Error()))
	}
}

func (ms *MultiSiteService) emitSiteUpdated(ctx context.Context, ev ports.SiteEvent, domain string) {
	if ms.eventLog == nil {
		return
	}
	e := events.NewSiteUpdated(events.SiteUpdatedParams{
		SiteName:   ev.SiteName,
		ConfigPath: ev.ConfigPath,
		Domain:     domain,
	})
	if err := ms.eventLog.Log(ctx, e); err != nil {
		ms.logger.Error("failed to emit site.updated event", slog.String("error", err.Error()))
	}
}

func (ms *MultiSiteService) emitSiteRemoved(ctx context.Context, ev ports.SiteEvent) {
	if ms.eventLog == nil {
		return
	}
	e := events.NewSiteRemoved(events.SiteRemovedParams{
		SiteName:   ev.SiteName,
		ConfigPath: ev.ConfigPath,
	})
	if err := ms.eventLog.Log(ctx, e); err != nil {
		ms.logger.Error("failed to emit site.removed event", slog.String("error", err.Error()))
	}
}

func (ms *MultiSiteService) emitSiteLoadFailed(ctx context.Context, ev ports.SiteEvent, reason string) {
	if ms.eventLog == nil {
		return
	}
	e := events.NewSiteLoadFailed(events.SiteLoadFailedParams{
		SiteName:   ev.SiteName,
		ConfigPath: ev.ConfigPath,
		Reason:     reason,
	})
	if err := ms.eventLog.Log(ctx, e); err != nil {
		ms.logger.Error("failed to emit site.load_failed event", slog.String("error", err.Error()))
	}
}
