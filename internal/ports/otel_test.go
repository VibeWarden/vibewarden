package ports_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/ports"
)

func TestDescriptionOf(t *testing.T) {
	tests := []struct {
		name string
		opts []ports.InstrumentOption
		want string
	}{
		{"empty", nil, ""},
		{"no description", []ports.InstrumentOption{ports.WithUnit("s")}, ""},
		{"with description", []ports.InstrumentOption{ports.WithDescription("total requests")}, "total requests"},
		{"first wins", []ports.InstrumentOption{ports.WithDescription("first"), ports.WithDescription("second")}, "first"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ports.DescriptionOf(tt.opts); got != tt.want {
				t.Errorf("DescriptionOf() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUnitOf(t *testing.T) {
	tests := []struct {
		name string
		opts []ports.InstrumentOption
		want string
	}{
		{"empty", nil, ""},
		{"no unit", []ports.InstrumentOption{ports.WithDescription("desc")}, ""},
		{"with unit", []ports.InstrumentOption{ports.WithUnit("ms")}, "ms"},
		{"first wins", []ports.InstrumentOption{ports.WithUnit("s"), ports.WithUnit("ms")}, "s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ports.UnitOf(tt.opts); got != tt.want {
				t.Errorf("UnitOf() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBucketsOf(t *testing.T) {
	tests := []struct {
		name string
		opts []ports.InstrumentOption
		want int
	}{
		{"empty", nil, 0},
		{"no buckets", []ports.InstrumentOption{ports.WithDescription("desc")}, 0},
		{"with buckets", []ports.InstrumentOption{ports.WithExplicitBuckets([]float64{0.01, 0.1, 1.0})}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ports.BucketsOf(tt.opts)
			if len(got) != tt.want {
				t.Errorf("BucketsOf() len = %d, want %d", len(got), tt.want)
			}
		})
	}
}
