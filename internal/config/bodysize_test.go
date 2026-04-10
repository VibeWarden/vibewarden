package config

import (
	"testing"
)

func TestParseBodySize(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{
			name:  "empty string returns zero (no limit)",
			input: "",
			want:  0,
		},
		{
			name:  "zero string returns zero (no limit)",
			input: "0",
			want:  0,
		},
		{
			name:  "bytes",
			input: "100B",
			want:  100,
		},
		{
			name:  "bytes without unit",
			input: "512",
			want:  512,
		},
		{
			name:  "kilobytes",
			input: "1KB",
			want:  1024,
		},
		{
			name:  "kilobytes lowercase",
			input: "1kb",
			want:  1024,
		},
		{
			name:  "512 kilobytes",
			input: "512KB",
			want:  524288,
		},
		{
			name:  "1 megabyte",
			input: "1MB",
			want:  1048576,
		},
		{
			name:  "50 megabytes",
			input: "50MB",
			want:  52428800,
		},
		{
			name:  "1 gigabyte",
			input: "1GB",
			want:  1073741824,
		},
		{
			name:  "1 terabyte",
			input: "1TB",
			want:  1099511627776,
		},
		{
			name:  "whitespace trimmed",
			input: " 10 MB ",
			want:  10485760,
		},
		{
			name:    "invalid unit",
			input:   "10PB",
			wantErr: true,
		},
		{
			name:    "no numeric value",
			input:   "MB",
			wantErr: true,
		},
		{
			name:    "negative value",
			input:   "-1MB",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseBodySize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseBodySize(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseBodySize(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
