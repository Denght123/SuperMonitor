package service

import "testing"

func TestBalanceStatusMarksEmptyBalanceCritical(t *testing.T) {
	tests := []struct {
		name      string
		remaining float64
		want      string
	}{
		{name: "zero", remaining: 0, want: "critical"},
		{name: "negative defensive value", remaining: -0.01, want: "critical"},
		{name: "positive balance", remaining: 0.01, want: "healthy"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := balanceStatus(test.remaining); got != test.want {
				t.Fatalf("balanceStatus(%v) = %q, want %q", test.remaining, got, test.want)
			}
		})
	}
}
