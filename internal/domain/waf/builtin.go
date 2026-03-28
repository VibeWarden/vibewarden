package waf

// builtinRuleSpec is a compile-time specification for a built-in rule.
type builtinRuleSpec struct {
	name     string
	pattern  string
	severity Severity
	category Category
}

// builtinSpecs holds all built-in WAF detection patterns shipped with
// VibeWarden. Patterns are compiled once at startup via BuiltinRules().
//
// Pattern authorship notes:
//   - All patterns are matched case-insensitively (the (?i) flag is added by NewRule).
//   - Patterns are intentionally broad to catch obfuscated variants; false-positive
//     tuning is left to the operator via rule exclusions (future work).
var builtinSpecs = []builtinRuleSpec{
	// -------------------------------------------------------------------------
	// SQL Injection
	// -------------------------------------------------------------------------

	// Tautology patterns: ' OR '1'='1, ' OR 1=1--, etc.
	{
		name:     "sqli-tautology",
		pattern:  `'[\s]*or[\s]+([\w'"]+[\s]*=[\s]*[\w'"]+|[\d]+[\s]*=[\s]*[\d]+)`,
		severity: SeverityCritical,
		category: CategorySQLInjection,
	},
	// UNION SELECT — used to append extra columns to a query result.
	{
		name:     "sqli-union-select",
		pattern:  `union[\s\+\/\*]+select`,
		severity: SeverityCritical,
		category: CategorySQLInjection,
	},
	// DROP / DELETE — destructive DDL/DML statements.
	{
		name:     "sqli-drop-delete",
		pattern:  `(drop|delete)[\s\+\/\*]+(table|database|from)`,
		severity: SeverityCritical,
		category: CategorySQLInjection,
	},
	// Generic SQL comment terminator injection (-- or #) often used to truncate queries.
	{
		name:     "sqli-comment-terminator",
		pattern:  `(--|#)[\s]*$`,
		severity: SeverityHigh,
		category: CategorySQLInjection,
	},
	// Stacked queries via semicolon.
	{
		name:     "sqli-stacked-query",
		pattern:  `;[\s]*(select|insert|update|delete|drop|create|alter|exec|execute)`,
		severity: SeverityHigh,
		category: CategorySQLInjection,
	},

	// -------------------------------------------------------------------------
	// Cross-Site Scripting (XSS)
	// -------------------------------------------------------------------------

	// <script> tag injection.
	{
		name:     "xss-script-tag",
		pattern:  `<[\s]*script[\s\S]*?>`,
		severity: SeverityCritical,
		category: CategoryXSS,
	},
	// javascript: URI scheme (href, src, action, etc.).
	{
		name:     "xss-javascript-uri",
		pattern:  `javascript[\s]*:`,
		severity: SeverityHigh,
		category: CategoryXSS,
	},
	// Inline event handler attributes: onclick, onerror, onload, onmouseover, etc.
	{
		name:     "xss-event-handler",
		pattern:  `\bon\w+[\s]*=`,
		severity: SeverityHigh,
		category: CategoryXSS,
	},
	// <img> / <iframe> / <svg> tags commonly used in XSS payloads.
	{
		name:     "xss-html-injection-tag",
		pattern:  `<[\s]*(img|iframe|svg|object|embed|link|meta)[\s\S]*?>`,
		severity: SeverityMedium,
		category: CategoryXSS,
	},
	// Expression/vbscript/data: URI schemes.
	{
		name:     "xss-dangerous-scheme",
		pattern:  `(vbscript|expression|data:text/html)[\s]*:`,
		severity: SeverityHigh,
		category: CategoryXSS,
	},

	// -------------------------------------------------------------------------
	// Path Traversal
	// -------------------------------------------------------------------------

	// Directory traversal sequences: ../ and ..\
	{
		name:     "path-traversal-dotdot",
		pattern:  `(\.\.[\\/]){1,}`,
		severity: SeverityHigh,
		category: CategoryPathTraversal,
	},
	// URL-encoded traversal: %2e%2e%2f or %2e%2e%5c
	{
		name:     "path-traversal-encoded",
		pattern:  `(%2e%2e|%252e%252e)[%2f%5c\/\\]`,
		severity: SeverityHigh,
		category: CategoryPathTraversal,
	},
	// Direct access to sensitive Unix files.
	{
		name:     "path-traversal-etc-passwd",
		pattern:  `/etc/(passwd|shadow|hosts|hostname|issue|group|crontab)`,
		severity: SeverityCritical,
		category: CategoryPathTraversal,
	},
	// Windows sensitive paths.
	{
		name:     "path-traversal-windows-system",
		pattern:  `(windows[\\/]system32|winnt[\\/]system32|boot\.ini)`,
		severity: SeverityCritical,
		category: CategoryPathTraversal,
	},

	// -------------------------------------------------------------------------
	// Command Injection
	// -------------------------------------------------------------------------

	// Semicolon followed by common Unix commands.
	{
		name:     "cmdi-semicolon",
		pattern:  `;[\s]*(ls|cat|id|whoami|uname|pwd|echo|curl|wget|bash|sh|nc|python|perl|ruby)(?:\s|$|/)`,
		severity: SeverityCritical,
		category: CategoryCommandInjection,
	},
	// Pipe to common Unix commands.
	{
		name:     "cmdi-pipe",
		pattern:  `\|[\s]*(ls|cat|id|whoami|uname|pwd|echo|curl|wget|bash|sh|nc|python|perl|ruby)(?:\s|$|/)`,
		severity: SeverityCritical,
		category: CategoryCommandInjection,
	},
	// Backtick command substitution.
	{
		name:     "cmdi-backtick",
		pattern:  "`[^`]+`",
		severity: SeverityHigh,
		category: CategoryCommandInjection,
	},
	// $() command substitution.
	{
		name:     "cmdi-dollar-paren",
		pattern:  `\$\([^)]+\)`,
		severity: SeverityHigh,
		category: CategoryCommandInjection,
	},
	// Logical AND/OR chaining (cmd && cmd, cmd || cmd).
	{
		name:     "cmdi-logical-chain",
		pattern:  `(&&|\|\|)[\s]*(ls|cat|id|whoami|uname|pwd|echo|curl|wget|bash|sh|nc|python|perl|ruby)(?:\s|$|/)`,
		severity: SeverityCritical,
		category: CategoryCommandInjection,
	},
}

// BuiltinRules returns the compiled set of built-in WAF detection rules.
// It panics if any built-in pattern fails to compile — this would be a
// programming error that must be caught at startup (the patterns are static).
func BuiltinRules() []Rule {
	rules := make([]Rule, 0, len(builtinSpecs))
	for _, spec := range builtinSpecs {
		r, err := NewRule(spec.name, spec.pattern, spec.severity, spec.category)
		if err != nil {
			// Static patterns must always compile. Panic is acceptable here because
			// this is equivalent to a startup configuration error in main().
			panic("waf: invalid built-in rule " + spec.name + ": " + err.Error())
		}
		rules = append(rules, r)
	}
	return rules
}
