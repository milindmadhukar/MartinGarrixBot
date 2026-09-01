package commands

import (
	"errors"
	"testing"
)

func TestResolveAmount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		balance int64
		amt     int
		amtOk   bool
		isAll   bool
		isHalf  bool
		want    int64
		wantErr error
	}{
		{
			name:    "an explicit amount within the balance",
			balance: 1000,
			amt:     250,
			amtOk:   true,
			want:    250,
		},
		{
			name:    "an explicit amount equal to the balance",
			balance: 1000,
			amt:     1000,
			amtOk:   true,
			want:    1000,
		},
		{
			name:    "all takes the whole balance",
			balance: 1000,
			isAll:   true,
			want:    1000,
		},
		{
			name:    "half rounds down",
			balance: 1001,
			isHalf:  true,
			want:    500,
		},
		{
			name:    "half of an even balance",
			balance: 1000,
			isHalf:  true,
			want:    500,
		},
		{
			// Both flags together is not rejected; half is evaluated first, so
			// it wins. Recorded because it is a coin-flip either way.
			name:    "half wins when both all and half are given",
			balance: 1000,
			isAll:   true,
			isHalf:  true,
			want:    500,
		},
		{
			name:    "an explicit amount is ignored when half is also given",
			balance: 1000,
			amt:     10,
			amtOk:   true,
			isHalf:  true,
			want:    500,
		},
		{
			name:    "more than the balance is refused",
			balance: 100,
			amt:     101,
			amtOk:   true,
			wantErr: ErrInsufficientBalance,
		},
		{
			name:    "zero is refused",
			balance: 100,
			amt:     0,
			amtOk:   true,
			wantErr: ErrAmountNotPositive,
		},
		{
			name:    "a negative amount is refused",
			balance: 100,
			amt:     -50,
			amtOk:   true,
			wantErr: ErrAmountNotPositive,
		},
		{
			name:    "no option at all is refused",
			balance: 100,
			wantErr: ErrNoAmountGiven,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := resolveAmount(tt.balance, tt.amt, tt.amtOk, tt.isAll, tt.isHalf)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("resolveAmount() = %d, want %d", got, tt.want)
			}
		})
	}
}

// BUG: "all" or "half" on an empty balance resolves to zero rather than an
// error, and the handlers then report "Successfully gave 0 coins". Harmless but
// confusing. Pinned so a future fix is a deliberate change rather than a
// surprise.
func TestResolveAmount_AllOnAnEmptyBalanceSucceedsWithZero(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		isAll  bool
		isHalf bool
	}{
		{"all", true, false},
		{"half", false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := resolveAmount(0, 0, false, tt.isAll, tt.isHalf)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != 0 {
				t.Errorf("resolveAmount() = %d, want 0", got)
			}
		})
	}
}

// The resolved amount must never exceed the balance, or a command would move
// coins a member does not have.
func TestResolveAmount_NeverExceedsTheBalance(t *testing.T) {
	t.Parallel()

	for balance := int64(0); balance <= 200; balance++ {
		for _, flags := range []struct{ isAll, isHalf bool }{{true, false}, {false, true}} {
			got, err := resolveAmount(balance, 0, false, flags.isAll, flags.isHalf)
			if err != nil {
				t.Fatalf("balance %d: unexpected error: %v", balance, err)
			}
			if got < 0 || got > balance {
				t.Fatalf("balance %d resolved to %d, outside [0, %d]", balance, got, balance)
			}
		}

		for amt := 0; amt <= 200; amt++ {
			got, err := resolveAmount(balance, amt, true, false, false)
			if err != nil {
				continue // refused, which is a valid outcome
			}
			if got > balance {
				t.Fatalf("balance %d, amount %d resolved to %d, over the balance",
					balance, amt, got)
			}
		}
	}
}
