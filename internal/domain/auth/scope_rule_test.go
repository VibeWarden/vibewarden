package auth

import (
	"testing"
)

func TestScopeRule_Matches(t *testing.T) {
	tests := []struct {
		name        string
		rule        ScopeRule
		method      string
		requestPath string
		want        bool
	}{
		{
			name:        "exact path match, any method (empty methods)",
			rule:        ScopeRule{Path: "/admin", Methods: nil, RequiredScopes: []string{"admin"}},
			method:      "GET",
			requestPath: "/admin",
			want:        true,
		},
		{
			name:        "wildcard path match, any method",
			rule:        ScopeRule{Path: "/api/v1/*", Methods: nil, RequiredScopes: []string{"read"}},
			method:      "POST",
			requestPath: "/api/v1/users",
			want:        true,
		},
		{
			name:        "wildcard path no match — wrong segment count",
			rule:        ScopeRule{Path: "/api/v1/*", Methods: nil, RequiredScopes: []string{"read"}},
			method:      "GET",
			requestPath: "/api/v2/users",
			want:        false,
		},
		{
			name:        "method match",
			rule:        ScopeRule{Path: "/api/*", Methods: []string{"GET", "HEAD"}, RequiredScopes: []string{"read"}},
			method:      "GET",
			requestPath: "/api/data",
			want:        true,
		},
		{
			name:        "method mismatch",
			rule:        ScopeRule{Path: "/api/*", Methods: []string{"GET", "HEAD"}, RequiredScopes: []string{"read"}},
			method:      "POST",
			requestPath: "/api/data",
			want:        false,
		},
		{
			name:        "path mismatch",
			rule:        ScopeRule{Path: "/admin/*", Methods: nil, RequiredScopes: []string{"admin"}},
			method:      "GET",
			requestPath: "/api/data",
			want:        false,
		},
		{
			name:        "invalid path pattern never matches",
			rule:        ScopeRule{Path: "[invalid", Methods: nil, RequiredScopes: []string{"read"}},
			method:      "GET",
			requestPath: "/api/data",
			want:        false,
		},
		{
			name:        "empty methods list means all methods allowed",
			rule:        ScopeRule{Path: "/*", Methods: []string{}, RequiredScopes: []string{"read"}},
			method:      "DELETE",
			requestPath: "/anything",
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.rule.Matches(tt.method, tt.requestPath)
			if got != tt.want {
				t.Errorf("ScopeRule.Matches(%q, %q) = %v, want %v",
					tt.method, tt.requestPath, got, tt.want)
			}
		})
	}
}

func TestScopeRule_SatisfiedBy(t *testing.T) {
	tests := []struct {
		name           string
		requiredScopes []string
		keyScopes      []Scope
		want           bool
	}{
		{
			name:           "all required scopes present",
			requiredScopes: []string{"read", "write"},
			keyScopes:      []Scope{"read", "write", "admin"},
			want:           true,
		},
		{
			name:           "missing one required scope",
			requiredScopes: []string{"read", "write"},
			keyScopes:      []Scope{"read"},
			want:           false,
		},
		{
			name:           "no required scopes — always satisfied",
			requiredScopes: []string{},
			keyScopes:      []Scope{},
			want:           true,
		},
		{
			name:           "key has no scopes but rule requires some",
			requiredScopes: []string{"admin"},
			keyScopes:      []Scope{},
			want:           false,
		},
		{
			name:           "single scope match",
			requiredScopes: []string{"read"},
			keyScopes:      []Scope{"read"},
			want:           true,
		},
		{
			name:           "key scopes are nil",
			requiredScopes: []string{"read"},
			keyScopes:      nil,
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := ScopeRule{RequiredScopes: tt.requiredScopes}
			got := rule.SatisfiedBy(tt.keyScopes)
			if got != tt.want {
				t.Errorf("ScopeRule.SatisfiedBy(%v) = %v, want %v", tt.keyScopes, got, tt.want)
			}
		})
	}
}

func TestMatchingScopeRule(t *testing.T) {
	rules := []ScopeRule{
		{Path: "/api/v1/*", Methods: []string{"GET", "HEAD"}, RequiredScopes: []string{"read"}},
		{Path: "/api/v1/*", Methods: []string{"POST", "PUT", "DELETE"}, RequiredScopes: []string{"write"}},
		{Path: "/admin/*", RequiredScopes: []string{"admin"}},
	}

	tests := []struct {
		name        string
		method      string
		requestPath string
		wantFound   bool
		wantScopes  []string
	}{
		{
			name:        "matches first read rule",
			method:      "GET",
			requestPath: "/api/v1/users",
			wantFound:   true,
			wantScopes:  []string{"read"},
		},
		{
			name:        "matches write rule",
			method:      "POST",
			requestPath: "/api/v1/users",
			wantFound:   true,
			wantScopes:  []string{"write"},
		},
		{
			name:        "matches admin rule (no method restriction)",
			method:      "DELETE",
			requestPath: "/admin/settings",
			wantFound:   true,
			wantScopes:  []string{"admin"},
		},
		{
			name:        "no rule matches",
			method:      "GET",
			requestPath: "/public/health",
			wantFound:   false,
		},
		{
			name:        "path matches but method does not match any rule",
			method:      "PATCH",
			requestPath: "/api/v1/users",
			wantFound:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule, found := MatchingScopeRule(rules, tt.method, tt.requestPath)
			if found != tt.wantFound {
				t.Errorf("MatchingScopeRule found = %v, want %v", found, tt.wantFound)
				return
			}
			if found && len(rule.RequiredScopes) != len(tt.wantScopes) {
				t.Errorf("RequiredScopes = %v, want %v", rule.RequiredScopes, tt.wantScopes)
			}
		})
	}
}

func TestMatchingScopeRule_Empty(t *testing.T) {
	rule, found := MatchingScopeRule(nil, "GET", "/anything")
	if found {
		t.Errorf("expected no match on empty rules, got %v", rule)
	}
}

func TestValidateScopeRules(t *testing.T) {
	tests := []struct {
		name    string
		rules   []ScopeRule
		wantErr bool
	}{
		{
			name: "valid rules",
			rules: []ScopeRule{
				{Path: "/api/*", RequiredScopes: []string{"read"}},
				{Path: "/admin/*", RequiredScopes: []string{"admin"}},
			},
			wantErr: false,
		},
		{
			name:    "empty rules",
			rules:   nil,
			wantErr: false,
		},
		{
			name: "invalid path pattern",
			rules: []ScopeRule{
				{Path: "[invalid", RequiredScopes: []string{"read"}},
			},
			wantErr: true,
		},
		{
			name: "valid wildcard patterns",
			rules: []ScopeRule{
				{Path: "/*", RequiredScopes: []string{"any"}},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateScopeRules(tt.rules)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateScopeRules() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
