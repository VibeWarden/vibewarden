package dockerfile_test

import (
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/dockerfile"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantStages      int
		wantMultiStage  bool
		wantExposes     []int
		wantHealthcheck bool
		wantFinalUser   string
		wantFirstImage  string
		wantFirstTag    string
		wantFirstAlias  string
		wantFirstDigest string
	}{
		{
			name:           "single stage alpine",
			input:          "FROM alpine:latest\nEXPOSE 3000\nCMD [\"./app\"]\n",
			wantStages:     1,
			wantMultiStage: false,
			wantExposes:    []int{3000},
			wantFirstImage: "alpine",
			wantFirstTag:   "latest",
		},
		{
			name: "multi-stage go builder",
			input: "FROM golang:1.26-alpine AS builder\n" +
				"RUN go build -o /app .\n" +
				"FROM alpine:latest\n" +
				"COPY --from=builder /app /app\n" +
				"EXPOSE 3000\n",
			wantStages:     2,
			wantMultiStage: true,
			wantExposes:    []int{3000},
			wantFirstImage: "golang",
			wantFirstTag:   "1.26-alpine",
			wantFirstAlias: "builder",
		},
		{
			name:           "no tag normalised to latest",
			input:          "FROM golang\n",
			wantStages:     1,
			wantMultiStage: false,
			wantFirstImage: "golang",
			wantFirstTag:   "latest",
		},
		{
			name:            "HEALTHCHECK present",
			input:           "FROM alpine:latest\nHEALTHCHECK CMD wget -q http://localhost/health\n",
			wantStages:      1,
			wantHealthcheck: true,
			wantFirstImage:  "alpine",
			wantFirstTag:    "latest",
		},
		{
			name:           "USER non-root in final stage",
			input:          "FROM alpine:latest\nUSER nonroot\nCMD [\"./app\"]\n",
			wantStages:     1,
			wantFinalUser:  "nonroot",
			wantFirstImage: "alpine",
			wantFirstTag:   "latest",
		},
		{
			name: "USER in builder only — final stage has no user",
			input: "FROM golang:1.26-alpine AS builder\n" +
				"USER builduser\n" +
				"RUN go build .\n" +
				"FROM alpine:latest\n",
			wantStages:     2,
			wantMultiStage: true,
			wantFinalUser:  "", // final stage sets no USER
			wantFirstImage: "golang",
			wantFirstTag:   "1.26-alpine",
		},
		{
			name:           "EXPOSE with /tcp suffix stripped",
			input:          "FROM alpine:latest\nEXPOSE 3000/tcp\n",
			wantStages:     1,
			wantExposes:    []int{3000},
			wantFirstImage: "alpine",
			wantFirstTag:   "latest",
		},
		{
			name:           "multiple EXPOSE picks all",
			input:          "FROM alpine:latest\nEXPOSE 3000\nEXPOSE 8080\n",
			wantStages:     1,
			wantExposes:    []int{3000, 8080},
			wantFirstImage: "alpine",
			wantFirstTag:   "latest",
		},
		{
			name:           "malformed EXPOSE skipped",
			input:          "FROM alpine:latest\nEXPOSE notaport\n",
			wantStages:     1,
			wantExposes:    nil,
			wantFirstImage: "alpine",
			wantFirstTag:   "latest",
		},
		{
			name:            "digest-pinned FROM",
			input:           "FROM alpine@sha256:abc123def456\n",
			wantStages:      1,
			wantFirstImage:  "alpine",
			wantFirstTag:    "",
			wantFirstDigest: "sha256:abc123def456",
		},
		{
			name:           "platform flag stripped",
			input:          "FROM --platform=linux/amd64 golang:1.26-alpine AS builder\n",
			wantStages:     1,
			wantFirstImage: "golang",
			wantFirstTag:   "1.26-alpine",
			wantFirstAlias: "builder",
		},
		{
			name:           "registry prefix stripped",
			input:          "FROM gcr.io/distroless/base:latest\n",
			wantStages:     1,
			wantFirstImage: "distroless/base",
			wantFirstTag:   "latest",
		},
		{
			name:           "comments and blank lines skipped",
			input:          "# This is a Dockerfile\n\nFROM alpine:latest\n# another comment\nEXPOSE 3000\n",
			wantStages:     1,
			wantExposes:    []int{3000},
			wantFirstImage: "alpine",
			wantFirstTag:   "latest",
		},
		{
			name:           "continuation line skipped",
			input:          "FROM alpine:latest\nRUN echo hello \\\n  && echo world\nEXPOSE 3000\n",
			wantStages:     1,
			wantExposes:    []int{3000},
			wantFirstImage: "alpine",
			wantFirstTag:   "latest",
		},
		{
			name:           "USER root is captured",
			input:          "FROM alpine:latest\nUSER root\n",
			wantStages:     1,
			wantFinalUser:  "root",
			wantFirstImage: "alpine",
			wantFirstTag:   "latest",
		},
		{
			name:           "USER 0 is captured",
			input:          "FROM alpine:latest\nUSER 0\n",
			wantStages:     1,
			wantFinalUser:  "0",
			wantFirstImage: "alpine",
			wantFirstTag:   "latest",
		},
		{
			name:           "case-insensitive instructions",
			input:          "from alpine:latest\nexpose 4000\n",
			wantStages:     1,
			wantExposes:    []int{4000},
			wantFirstImage: "alpine",
			wantFirstTag:   "latest",
		},
		{
			name:           "docker hub image with namespace",
			input:          "FROM node:20-alpine AS base\n",
			wantStages:     1,
			wantFirstImage: "node",
			wantFirstTag:   "20-alpine",
			wantFirstAlias: "base",
		},
		{
			name:           "empty input",
			input:          "",
			wantStages:     0,
			wantMultiStage: false,
		},
		{
			name:           "private registry with port",
			input:          "FROM registry:5000/myapp:latest\n",
			wantStages:     1,
			wantFirstImage: "myapp",
			wantFirstTag:   "latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := dockerfile.Parse(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("Parse() unexpected error: %v", err)
			}

			if len(p.Stages) != tt.wantStages {
				t.Errorf("len(Stages) = %d, want %d", len(p.Stages), tt.wantStages)
			}
			if p.IsMultiStage != tt.wantMultiStage {
				t.Errorf("IsMultiStage = %v, want %v", p.IsMultiStage, tt.wantMultiStage)
			}
			if p.HasHealthcheck != tt.wantHealthcheck {
				t.Errorf("HasHealthcheck = %v, want %v", p.HasHealthcheck, tt.wantHealthcheck)
			}
			if p.FinalUser != tt.wantFinalUser {
				t.Errorf("FinalUser = %q, want %q", p.FinalUser, tt.wantFinalUser)
			}

			// Check expose ports.
			if len(tt.wantExposes) > 0 {
				if len(p.Exposes) != len(tt.wantExposes) {
					t.Errorf("len(Exposes) = %d, want %d", len(p.Exposes), len(tt.wantExposes))
				} else {
					for i, want := range tt.wantExposes {
						if p.Exposes[i].Port != want {
							t.Errorf("Exposes[%d].Port = %d, want %d", i, p.Exposes[i].Port, want)
						}
					}
				}
			} else if len(p.Exposes) != 0 && tt.wantExposes == nil {
				t.Errorf("Exposes = %v, want empty", p.Exposes)
			}

			// Check first stage fields when expected.
			if tt.wantFirstImage != "" && len(p.Stages) > 0 {
				s := p.Stages[0]
				if s.Image != tt.wantFirstImage {
					t.Errorf("Stages[0].Image = %q, want %q", s.Image, tt.wantFirstImage)
				}
				if tt.wantFirstTag != "" && s.Tag != tt.wantFirstTag {
					t.Errorf("Stages[0].Tag = %q, want %q", s.Tag, tt.wantFirstTag)
				}
				if tt.wantFirstAlias != "" && s.Alias != tt.wantFirstAlias {
					t.Errorf("Stages[0].Alias = %q, want %q", s.Alias, tt.wantFirstAlias)
				}
				if tt.wantFirstDigest != "" && s.Digest != tt.wantFirstDigest {
					t.Errorf("Stages[0].Digest = %q, want %q", s.Digest, tt.wantFirstDigest)
				}
			}
		})
	}
}
