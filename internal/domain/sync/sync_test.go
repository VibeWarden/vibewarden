package sync

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// StateType
// ---------------------------------------------------------------------------

func TestStateType_Validate(t *testing.T) {
	tests := []struct {
		name    string
		input   StateType
		wantErr bool
	}{
		{"rate_limit", StateTypeRateLimit, false},
		{"ip_blocklist", StateTypeIPBlocklist, false},
		{"circuit_breaker", StateTypeCircuitBreaker, false},
		{"unknown", StateType("unknown"), true},
		{"empty", StateType(""), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestStateType_String(t *testing.T) {
	tests := []struct {
		input StateType
		want  string
	}{
		{StateTypeRateLimit, "rate_limit"},
		{StateTypeIPBlocklist, "ip_blocklist"},
		{StateTypeCircuitBreaker, "circuit_breaker"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.input.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Counter
// ---------------------------------------------------------------------------

func TestNewCounter(t *testing.T) {
	c := NewCounter()
	if c.Get() != 0 {
		t.Errorf("NewCounter().Get() = %d, want 0", c.Get())
	}
}

func TestNewCounterWithValue(t *testing.T) {
	tests := []struct {
		name    string
		value   int64
		wantErr bool
	}{
		{"zero", 0, false},
		{"positive", 42, false},
		{"negative", -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := NewCounterWithValue(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewCounterWithValue(%d) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
			if err == nil && c.Get() != tt.value {
				t.Errorf("Get() = %d, want %d", c.Get(), tt.value)
			}
		})
	}
}

func TestCounter_Increment(t *testing.T) {
	tests := []struct {
		name      string
		initial   int64
		delta     int64
		wantValue int64
		wantErr   bool
	}{
		{"increment by one", 0, 1, 1, false},
		{"increment by ten", 5, 10, 15, false},
		{"zero delta", 0, 0, 0, true},
		{"negative delta", 0, -1, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := NewCounterWithValue(tt.initial)
			got, err := c.Increment(tt.delta)
			if (err != nil) != tt.wantErr {
				t.Errorf("Increment(%d) error = %v, wantErr %v", tt.delta, err, tt.wantErr)
			}
			if err == nil && got != tt.wantValue {
				t.Errorf("Increment(%d) = %d, want %d", tt.delta, got, tt.wantValue)
			}
		})
	}
}

func TestCounter_Increment_Accumulates(t *testing.T) {
	c := NewCounter()

	for i := int64(1); i <= 5; i++ {
		got, err := c.Increment(i)
		if err != nil {
			t.Fatalf("Increment(%d): unexpected error: %v", i, err)
		}
		// sum of 1+2+3+4+5 incrementally: 1, 3, 6, 10, 15
		want := i * (i + 1) / 2
		if got != want {
			t.Errorf("after Increment(%d): got %d, want %d", i, got, want)
		}
	}
}

func TestCounter_Reset(t *testing.T) {
	c, _ := NewCounterWithValue(10)
	prev := c.Reset()
	if prev != 10 {
		t.Errorf("Reset() previous = %d, want 10", prev)
	}
	if c.Get() != 0 {
		t.Errorf("after Reset(), Get() = %d, want 0", c.Get())
	}
}

// ---------------------------------------------------------------------------
// Set
// ---------------------------------------------------------------------------

func TestNewSet(t *testing.T) {
	s := NewSet()
	if s.Size() != 0 {
		t.Errorf("NewSet().Size() = %d, want 0", s.Size())
	}
}

func TestSet_Add(t *testing.T) {
	tests := []struct {
		name    string
		member  string
		wantErr bool
	}{
		{"valid member", "192.168.1.1", false},
		{"empty member", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSet()
			err := s.Add(tt.member)
			if (err != nil) != tt.wantErr {
				t.Errorf("Add(%q) error = %v, wantErr %v", tt.member, err, tt.wantErr)
			}
			if err == nil && !s.Contains(tt.member) {
				t.Errorf("after Add(%q), Contains() = false, want true", tt.member)
			}
		})
	}
}

func TestSet_Add_Idempotent(t *testing.T) {
	s := NewSet()
	_ = s.Add("10.0.0.1")
	_ = s.Add("10.0.0.1")
	if s.Size() != 1 {
		t.Errorf("adding duplicate: Size() = %d, want 1", s.Size())
	}
}

func TestSet_Remove(t *testing.T) {
	tests := []struct {
		name       string
		member     string
		addFirst   bool
		wantErr    bool
		wantExists bool
	}{
		{"remove existing", "10.0.0.1", true, false, false},
		{"remove non-existent is no-op", "10.0.0.2", false, false, false},
		{"empty member", "", false, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSet()
			if tt.addFirst {
				_ = s.Add(tt.member)
			}
			err := s.Remove(tt.member)
			if (err != nil) != tt.wantErr {
				t.Errorf("Remove(%q) error = %v, wantErr %v", tt.member, err, tt.wantErr)
			}
			if err == nil && s.Contains(tt.member) != tt.wantExists {
				t.Errorf("Contains(%q) = %v, want %v", tt.member, s.Contains(tt.member), tt.wantExists)
			}
		})
	}
}

func TestSet_Contains(t *testing.T) {
	s := NewSet()
	_ = s.Add("present")

	tests := []struct {
		member string
		want   bool
	}{
		{"present", true},
		{"absent", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.member, func(t *testing.T) {
			if got := s.Contains(tt.member); got != tt.want {
				t.Errorf("Contains(%q) = %v, want %v", tt.member, got, tt.want)
			}
		})
	}
}

func TestSet_Size(t *testing.T) {
	s := NewSet()
	if s.Size() != 0 {
		t.Errorf("empty set: Size() = %d, want 0", s.Size())
	}
	_ = s.Add("a")
	_ = s.Add("b")
	if s.Size() != 2 {
		t.Errorf("after 2 adds: Size() = %d, want 2", s.Size())
	}
	_ = s.Remove("a")
	if s.Size() != 1 {
		t.Errorf("after remove: Size() = %d, want 1", s.Size())
	}
}

// ---------------------------------------------------------------------------
// StateUpdate
// ---------------------------------------------------------------------------

func TestStateUpdate_Validate(t *testing.T) {
	tests := []struct {
		name    string
		update  StateUpdate
		wantErr bool
	}{
		{
			name: "valid counter update",
			update: StateUpdate{
				Type:  StateTypeRateLimit,
				Key:   "192.168.1.1",
				Delta: 1,
			},
			wantErr: false,
		},
		{
			name: "valid set update",
			update: StateUpdate{
				Type:    StateTypeIPBlocklist,
				Key:     "blocklist-main",
				Members: []string{"1.2.3.4"},
			},
			wantErr: false,
		},
		{
			name: "valid update with TTL",
			update: StateUpdate{
				Type:  StateTypeRateLimit,
				Key:   "user:42",
				Delta: 5,
				TTL:   time.Minute,
			},
			wantErr: false,
		},
		{
			name: "unknown type",
			update: StateUpdate{
				Type:  StateType("bogus"),
				Key:   "key",
				Delta: 1,
			},
			wantErr: true,
		},
		{
			name: "empty key",
			update: StateUpdate{
				Type:  StateTypeRateLimit,
				Key:   "",
				Delta: 1,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.update.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SyncMessage
// ---------------------------------------------------------------------------

func TestSyncMessage_Validate(t *testing.T) {
	validUpdate := StateUpdate{
		Type:  StateTypeRateLimit,
		Key:   "key",
		Delta: 1,
	}

	tests := []struct {
		name    string
		msg     SyncMessage
		wantErr bool
	}{
		{
			name:    "valid message",
			msg:     SyncMessage{Type: StateTypeRateLimit, InstanceID: "instance-1", Update: validUpdate},
			wantErr: false,
		},
		{
			name:    "empty instance ID",
			msg:     SyncMessage{Type: StateTypeRateLimit, InstanceID: "", Update: validUpdate},
			wantErr: true,
		},
		{
			name: "invalid update",
			msg: SyncMessage{
				Type:       StateTypeRateLimit,
				InstanceID: "instance-1",
				Update: StateUpdate{
					Type:  StateType("bad"),
					Key:   "key",
					Delta: 1,
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
