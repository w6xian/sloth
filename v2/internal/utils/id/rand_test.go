package id

import (
	"testing"
)

func TestParseSchema(t *testing.T) {

	tests := []struct {
		name string
		l    int
	}{
		{
			name: "rand-10",
			l:    10,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RandStr(tt.l); len(got) != tt.l {
				t.Errorf("RandStr() = %v, want %v", got, tt.l)
			}
		})
	}
}
