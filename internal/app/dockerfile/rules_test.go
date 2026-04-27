package dockerfile_test

import (
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/dockerfile"
)

// parsedFrom is a helper that parses a Dockerfile string and fatals on error.
func parsedFrom(t *testing.T, src string) dockerfile.Parsed {
	t.Helper()
	p, err := dockerfile.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	return p
}

// ─── Rule 1: Alpine base ─────────────────────────────────────────────────────

func TestRuleAlpineBase(t *testing.T) {
	tests := []struct {
		name       string
		dockerfile string
		wantState  dockerfile.Severity
		wantFrag   string
	}{
		{
			name:       "alpine image is OK",
			dockerfile: "FROM alpine:latest\n",
			wantState:  dockerfile.SeverityOK,
		},
		{
			name:       "golang alpine tag is OK",
			dockerfile: "FROM golang:1.26-alpine AS builder\nFROM alpine:latest\n",
			wantState:  dockerfile.SeverityOK,
		},
		{
			name:       "node alpine tag is OK",
			dockerfile: "FROM node:20-alpine\n",
			wantState:  dockerfile.SeverityOK,
		},
		{
			name:       "python alpine tag is OK",
			dockerfile: "FROM python:3.11-alpine\n",
			wantState:  dockerfile.SeverityOK,
		},
		{
			name:       "distroless final stage is FAIL",
			dockerfile: "FROM golang:1.26-alpine AS builder\nFROM gcr.io/distroless/base:latest\n",
			wantState:  dockerfile.SeverityFail,
			wantFrag:   "alpine",
		},
		{
			name:       "scratch is FAIL",
			dockerfile: "FROM golang:1.26-alpine AS builder\nFROM scratch\n",
			wantState:  dockerfile.SeverityFail,
			wantFrag:   "alpine",
		},
		{
			name:       "ubuntu is FAIL",
			dockerfile: "FROM ubuntu:22.04\n",
			wantState:  dockerfile.SeverityFail,
			wantFrag:   "alpine",
		},
		{
			name:       "empty parsed is OFF",
			dockerfile: "",
			wantState:  dockerfile.SeverityOff,
		},
		{
			name:       "multi-stage: final stage tag contains alpine is OK",
			dockerfile: "FROM golang:1.26-alpine AS builder\nRUN go build .\nFROM alpine:3.20\n",
			wantState:  dockerfile.SeverityOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := parsedFrom(t, tt.dockerfile)
			got := dockerfile.RuleAlpineBase(p)
			if got.State != tt.wantState {
				t.Errorf("State = %q, want %q (detail: %q)", got.State, tt.wantState, got.Detail)
			}
			if tt.wantFrag != "" && !strings.Contains(got.Detail, tt.wantFrag) {
				t.Errorf("Detail %q missing fragment %q", got.Detail, tt.wantFrag)
			}
		})
	}
}

// ─── Rule 2: EXPOSE matches port ─────────────────────────────────────────────

func TestRuleExposeMatchesPort(t *testing.T) {
	tests := []struct {
		name         string
		dockerfile   string
		upstreamPort int
		wantState    dockerfile.Severity
		wantFrag     string
	}{
		{
			name:         "EXPOSE matches — OK",
			dockerfile:   "FROM alpine:latest\nEXPOSE 3000\n",
			upstreamPort: 3000,
			wantState:    dockerfile.SeverityOK,
		},
		{
			name:         "EXPOSE mismatch — FAIL",
			dockerfile:   "FROM alpine:latest\nEXPOSE 3000\n",
			upstreamPort: 8080,
			wantState:    dockerfile.SeverityFail,
			wantFrag:     "3000",
		},
		{
			name:         "no EXPOSE — OFF",
			dockerfile:   "FROM alpine:latest\n",
			upstreamPort: 3000,
			wantState:    dockerfile.SeverityOff,
		},
		{
			name:         "multiple EXPOSE — uses last",
			dockerfile:   "FROM alpine:latest\nEXPOSE 3000\nEXPOSE 8080\n",
			upstreamPort: 3000,
			wantState:    dockerfile.SeverityFail,
			wantFrag:     "8080",
		},
		{
			name:         "EXPOSE with /tcp suffix — stripped",
			dockerfile:   "FROM alpine:latest\nEXPOSE 3000/tcp\n",
			upstreamPort: 3000,
			wantState:    dockerfile.SeverityOK,
		},
		{
			name:         "empty parsed — OFF",
			dockerfile:   "",
			upstreamPort: 3000,
			wantState:    dockerfile.SeverityOff,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := parsedFrom(t, tt.dockerfile)
			got := dockerfile.RuleExposeMatchesPort(p, tt.upstreamPort)
			if got.State != tt.wantState {
				t.Errorf("State = %q, want %q (detail: %q)", got.State, tt.wantState, got.Detail)
			}
			if tt.wantFrag != "" && !strings.Contains(got.Detail, tt.wantFrag) {
				t.Errorf("Detail %q missing fragment %q", got.Detail, tt.wantFrag)
			}
		})
	}
}

// ─── Rule 3: No HEALTHCHECK ──────────────────────────────────────────────────

func TestRuleNoHealthcheck(t *testing.T) {
	tests := []struct {
		name       string
		dockerfile string
		wantState  dockerfile.Severity
		wantFrag   string
	}{
		{
			name:       "no HEALTHCHECK — OK",
			dockerfile: "FROM alpine:latest\nRUN echo ok\n",
			wantState:  dockerfile.SeverityOK,
		},
		{
			name:       "HEALTHCHECK present — FAIL",
			dockerfile: "FROM alpine:latest\nHEALTHCHECK CMD wget -q http://localhost/health\n",
			wantState:  dockerfile.SeverityFail,
			wantFrag:   "HEALTHCHECK",
		},
		{
			name:       "empty parsed — OFF",
			dockerfile: "",
			wantState:  dockerfile.SeverityOff,
		},
		{
			name:       "HEALTHCHECK NONE variant — FAIL",
			dockerfile: "FROM alpine:latest\nHEALTHCHECK NONE\n",
			wantState:  dockerfile.SeverityFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := parsedFrom(t, tt.dockerfile)
			got := dockerfile.RuleNoHealthcheck(p)
			if got.State != tt.wantState {
				t.Errorf("State = %q, want %q (detail: %q)", got.State, tt.wantState, got.Detail)
			}
			if tt.wantFrag != "" && !strings.Contains(got.Detail, tt.wantFrag) {
				t.Errorf("Detail %q missing fragment %q", got.Detail, tt.wantFrag)
			}
		})
	}
}

// ─── Rule 4: Non-root USER ───────────────────────────────────────────────────

func TestRuleNonRootUser(t *testing.T) {
	tests := []struct {
		name       string
		dockerfile string
		wantState  dockerfile.Severity
		wantFrag   string
	}{
		{
			name:       "non-root USER — OK",
			dockerfile: "FROM alpine:latest\nUSER nonroot\n",
			wantState:  dockerfile.SeverityOK,
		},
		{
			name:       "non-root UID — OK",
			dockerfile: "FROM alpine:latest\nUSER 1001\n",
			wantState:  dockerfile.SeverityOK,
		},
		{
			name:       "non-root user:group — OK",
			dockerfile: "FROM alpine:latest\nUSER nonroot:nonroot\n",
			wantState:  dockerfile.SeverityOK,
		},
		{
			name:       "no USER — WARN",
			dockerfile: "FROM alpine:latest\n",
			wantState:  dockerfile.SeverityWarn,
			wantFrag:   "non-root",
		},
		{
			name:       "USER root — WARN",
			dockerfile: "FROM alpine:latest\nUSER root\n",
			wantState:  dockerfile.SeverityWarn,
			wantFrag:   "non-root",
		},
		{
			name:       "USER 0 — WARN",
			dockerfile: "FROM alpine:latest\nUSER 0\n",
			wantState:  dockerfile.SeverityWarn,
			wantFrag:   "non-root",
		},
		{
			name:       "USER root:root — WARN",
			dockerfile: "FROM alpine:latest\nUSER root:root\n",
			wantState:  dockerfile.SeverityWarn,
			wantFrag:   "non-root",
		},
		{
			name:       "empty parsed — OFF",
			dockerfile: "",
			wantState:  dockerfile.SeverityOff,
		},
		{
			name: "multi-stage: final stage USER counts",
			dockerfile: "FROM golang:1.26-alpine AS builder\n" +
				"USER root\n" +
				"FROM alpine:latest\n" +
				"USER nonroot\n",
			wantState: dockerfile.SeverityOK,
		},
		{
			name: "multi-stage: final stage no USER — WARN",
			dockerfile: "FROM golang:1.26-alpine AS builder\n" +
				"USER nonroot\n" +
				"FROM alpine:latest\n",
			wantState: dockerfile.SeverityWarn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := parsedFrom(t, tt.dockerfile)
			got := dockerfile.RuleNonRootUser(p)
			if got.State != tt.wantState {
				t.Errorf("State = %q, want %q (detail: %q)", got.State, tt.wantState, got.Detail)
			}
			if tt.wantFrag != "" && !strings.Contains(got.Detail, tt.wantFrag) {
				t.Errorf("Detail %q missing fragment %q", got.Detail, tt.wantFrag)
			}
		})
	}
}

// ─── Rule 5: Multi-stage for compiled ─────────────────────────────────────────

func TestRuleMultiStageForCompiled(t *testing.T) {
	goToolchain := dockerfile.Toolchain{Lang: dockerfile.LangGo, Major: 1, Minor: 26, Source: "go.mod"}
	nodeToolchain := dockerfile.Toolchain{Lang: dockerfile.LangNode, Major: 20, Minor: 0, Source: ".nvmrc"}
	noToolchain := dockerfile.Toolchain{}

	tests := []struct {
		name       string
		dockerfile string
		tc         dockerfile.Toolchain
		wantState  dockerfile.Severity
		wantFrag   string
	}{
		{
			name:       "Go multi-stage — OK",
			dockerfile: "FROM golang:1.26-alpine AS builder\nRUN go build .\nFROM alpine:latest\n",
			tc:         goToolchain,
			wantState:  dockerfile.SeverityOK,
		},
		{
			name:       "Go single-stage — FAIL",
			dockerfile: "FROM golang:1.26-alpine\nRUN go build .\nEXPOSE 3000\n",
			tc:         goToolchain,
			wantState:  dockerfile.SeverityFail,
			wantFrag:   "multi-stage",
		},
		{
			name:       "Node single-stage — OFF (not compiled)",
			dockerfile: "FROM node:20-alpine\n",
			tc:         nodeToolchain,
			wantState:  dockerfile.SeverityOff,
		},
		{
			name:       "no toolchain — OFF",
			dockerfile: "FROM alpine:latest\n",
			tc:         noToolchain,
			wantState:  dockerfile.SeverityOff,
		},
		{
			name:       "empty parsed — OFF",
			dockerfile: "",
			tc:         goToolchain,
			wantState:  dockerfile.SeverityOff,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := parsedFrom(t, tt.dockerfile)
			got := dockerfile.RuleMultiStageForCompiled(p, tt.tc)
			if got.State != tt.wantState {
				t.Errorf("State = %q, want %q (detail: %q)", got.State, tt.wantState, got.Detail)
			}
			if tt.wantFrag != "" && !strings.Contains(got.Detail, tt.wantFrag) {
				t.Errorf("Detail %q missing fragment %q", got.Detail, tt.wantFrag)
			}
		})
	}
}

// ─── Rule 6: Toolchain version match ─────────────────────────────────────────

func TestRuleToolchainMatch(t *testing.T) {
	tests := []struct {
		name       string
		dockerfile string
		tc         dockerfile.Toolchain
		wantState  dockerfile.Severity
		wantFrag   string
	}{
		{
			name:       "Go version match — OK",
			dockerfile: "FROM golang:1.26-alpine AS builder\nFROM alpine:latest\n",
			tc:         dockerfile.Toolchain{Lang: dockerfile.LangGo, Major: 1, Minor: 26, Source: "go.mod"},
			wantState:  dockerfile.SeverityOK,
		},
		{
			name:       "Go version mismatch — FAIL (qr-dali bug)",
			dockerfile: "FROM golang:1.24-alpine AS builder\nFROM alpine:latest\n",
			tc:         dockerfile.Toolchain{Lang: dockerfile.LangGo, Major: 1, Minor: 26, Source: "go.mod"},
			wantState:  dockerfile.SeverityFail,
			wantFrag:   "1.24",
		},
		{
			name:       "Node version match — OK",
			dockerfile: "FROM node:20-alpine\n",
			tc:         dockerfile.Toolchain{Lang: dockerfile.LangNode, Major: 20, Minor: 0, Source: ".nvmrc"},
			wantState:  dockerfile.SeverityOK,
		},
		{
			name:       "Node version mismatch — FAIL",
			dockerfile: "FROM node:18-alpine\n",
			tc:         dockerfile.Toolchain{Lang: dockerfile.LangNode, Major: 20, Minor: 0, Source: ".nvmrc"},
			wantState:  dockerfile.SeverityFail,
			wantFrag:   "18",
		},
		{
			name:       "Python version match — OK",
			dockerfile: "FROM python:3.11-alpine\n",
			tc:         dockerfile.Toolchain{Lang: dockerfile.LangPython, Major: 3, Minor: 11, Source: ".python-version"},
			wantState:  dockerfile.SeverityOK,
		},
		{
			name:       "Python version mismatch — FAIL",
			dockerfile: "FROM python:3.9-alpine\n",
			tc:         dockerfile.Toolchain{Lang: dockerfile.LangPython, Major: 3, Minor: 11, Source: ".python-version"},
			wantState:  dockerfile.SeverityFail,
			wantFrag:   "3.9",
		},
		{
			name:       "builder tag is 'latest' — OFF",
			dockerfile: "FROM golang:latest AS builder\nFROM alpine:latest\n",
			tc:         dockerfile.Toolchain{Lang: dockerfile.LangGo, Major: 1, Minor: 26, Source: "go.mod"},
			wantState:  dockerfile.SeverityOff,
		},
		{
			name:       "builder tag is 'alpine' (no version) — OFF",
			dockerfile: "FROM golang:alpine AS builder\nFROM alpine:latest\n",
			tc:         dockerfile.Toolchain{Lang: dockerfile.LangGo, Major: 1, Minor: 26, Source: "go.mod"},
			wantState:  dockerfile.SeverityOff,
		},
		{
			name:       "no toolchain — OFF",
			dockerfile: "FROM golang:1.24-alpine AS builder\nFROM alpine:latest\n",
			tc:         dockerfile.Toolchain{},
			wantState:  dockerfile.SeverityOff,
		},
		{
			name:       "empty parsed — OFF",
			dockerfile: "",
			tc:         dockerfile.Toolchain{Lang: dockerfile.LangGo, Major: 1, Minor: 26, Source: "go.mod"},
			wantState:  dockerfile.SeverityOff,
		},
		{
			name:       "wrong image prefix for lang — OFF",
			dockerfile: "FROM node:20-alpine\n",
			tc:         dockerfile.Toolchain{Lang: dockerfile.LangGo, Major: 1, Minor: 26, Source: "go.mod"},
			wantState:  dockerfile.SeverityOff,
		},
		{
			name:       "hint mentions fix",
			dockerfile: "FROM golang:1.24-alpine AS builder\nFROM alpine:latest\n",
			tc:         dockerfile.Toolchain{Lang: dockerfile.LangGo, Major: 1, Minor: 26, Source: "go.mod"},
			wantState:  dockerfile.SeverityFail,
			wantFrag:   "1.26",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := parsedFrom(t, tt.dockerfile)
			got := dockerfile.RuleToolchainMatch(p, tt.tc)
			if got.State != tt.wantState {
				t.Errorf("State = %q, want %q (detail: %q)", got.State, tt.wantState, got.Detail)
			}
			if tt.wantFrag != "" && !strings.Contains(got.Detail, tt.wantFrag) {
				t.Errorf("Detail %q missing fragment %q", got.Detail, tt.wantFrag)
			}
		})
	}
}
