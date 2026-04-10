// Package csp provides a domain-level builder for Content-Security-Policy
// header values.
//
// Instead of writing a raw CSP string, callers populate a Config struct with
// per-directive source lists and call Build to obtain the serialised header
// value. The package has zero external dependencies — only the Go standard
// library is imported.
package csp

import (
	"strings"
)

// Config holds a declarative Content-Security-Policy configuration.
// Each field corresponds to a CSP fetch directive and contains the list of
// allowed sources for that directive.
//
// Sources are written verbatim into the header value, so keyword sources must
// include their surrounding single-quotes (e.g. "'self'", "'none'",
// "'unsafe-inline'").
//
// An empty slice for a directive means that directive is omitted from the
// generated header. To explicitly forbid a resource type, set the slice to
// []string{"'none'"}.
type Config struct {
	// DefaultSrc sets the default-src directive — the fallback for all fetch
	// directives that are not explicitly specified.
	DefaultSrc []string

	// ScriptSrc sets the script-src directive for JavaScript sources.
	ScriptSrc []string

	// StyleSrc sets the style-src directive for CSS sources.
	StyleSrc []string

	// ImgSrc sets the img-src directive for image sources.
	ImgSrc []string

	// ConnectSrc sets the connect-src directive for fetch/XHR/WebSocket targets.
	ConnectSrc []string

	// FontSrc sets the font-src directive for web font sources.
	FontSrc []string

	// FrameSrc sets the frame-src directive for nested browsing contexts
	// (iframes).
	FrameSrc []string

	// MediaSrc sets the media-src directive for audio and video sources.
	MediaSrc []string

	// ObjectSrc sets the object-src directive for plugin content (e.g. Flash).
	ObjectSrc []string

	// ManifestSrc sets the manifest-src directive for web app manifests.
	ManifestSrc []string

	// WorkerSrc sets the worker-src directive for Worker and SharedWorker
	// scripts.
	WorkerSrc []string

	// ChildSrc sets the child-src directive for web workers and nested
	// browsing contexts.
	ChildSrc []string

	// FormAction sets the form-action directive, restricting URLs that can be
	// used as HTML form targets.
	FormAction []string

	// FrameAncestors sets the frame-ancestors directive, controlling which
	// origins may embed this page in a frame.
	FrameAncestors []string

	// BaseURI sets the base-uri directive, restricting the URLs that can be
	// used in a <base> element.
	BaseURI []string
}

// directive is a (name, sources) pair used for ordered serialisation.
type directive struct {
	name    string
	sources []string
}

// Build serialises cfg into a Content-Security-Policy header value string.
// Directives with no configured sources are omitted. Directives are emitted
// in a deterministic order that matches the order of fields in Config.
// Build returns an empty string when no directives are configured.
func Build(cfg Config) string {
	directives := []directive{
		{"default-src", cfg.DefaultSrc},
		{"script-src", cfg.ScriptSrc},
		{"style-src", cfg.StyleSrc},
		{"img-src", cfg.ImgSrc},
		{"connect-src", cfg.ConnectSrc},
		{"font-src", cfg.FontSrc},
		{"frame-src", cfg.FrameSrc},
		{"media-src", cfg.MediaSrc},
		{"object-src", cfg.ObjectSrc},
		{"manifest-src", cfg.ManifestSrc},
		{"worker-src", cfg.WorkerSrc},
		{"child-src", cfg.ChildSrc},
		{"form-action", cfg.FormAction},
		{"frame-ancestors", cfg.FrameAncestors},
		{"base-uri", cfg.BaseURI},
	}

	parts := make([]string, 0, len(directives))
	for _, d := range directives {
		if len(d.sources) == 0 {
			continue
		}
		parts = append(parts, d.name+" "+strings.Join(d.sources, " "))
	}

	return strings.Join(parts, "; ")
}
