package library

import (
	"testing"
	"time"
)

func TestCompare(t *testing.T) {
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	late := early.Add(time.Minute)
	tests := []struct {
		name        string
		here, there Stamp
		want        Move
	}{
		{"same content stays, whatever the times", Stamp{early, "a"}, Stamp{late, "a"}, Stay},
		{"two untouched libraries stay", Stamp{}, Stamp{}, Stay},
		{"a later change is taken", Stamp{early, "a"}, Stamp{late, "b"}, Take},
		{"an earlier change is given the newer one", Stamp{late, "a"}, Stamp{early, "b"}, Give},
		{"a library you changed beats one you never did", Stamp{early, "a"}, Stamp{}, Give},
		{"an untouched library takes one you changed", Stamp{}, Stamp{early, "a"}, Take},
		{"a tie goes to the larger hash", Stamp{early, "a"}, Stamp{early, "b"}, Take},
		{"the other side of the tie agrees", Stamp{early, "b"}, Stamp{early, "a"}, Give},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Compare(tt.here, tt.there); got != tt.want {
				t.Fatalf("Compare = %v, want %v", got, tt.want)
			}
		})
	}
}
