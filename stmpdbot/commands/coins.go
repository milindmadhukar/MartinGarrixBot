package commands

import "errors"

// The coin commands all take the same three mutually-redundant options: an
// explicit amount, "all", and "half". /withdraw draws on the safe while
// /deposit and /give draw on coins in hand, but the arithmetic is identical,
// so it lives here rather than being copied into each handler.
var (
	// ErrNoAmountGiven means none of amount, all or half was supplied.
	ErrNoAmountGiven = errors.New("no amount given")

	// ErrAmountNotPositive means an explicit amount of zero or less was given.
	ErrAmountNotPositive = errors.New("amount must be positive")

	// ErrInsufficientBalance means the requested amount exceeds the balance.
	ErrInsufficientBalance = errors.New("insufficient balance")
)

// resolveAmount works out how many coins a command should move.
//
// half wins over all when both are passed, matching the order the handlers have
// always evaluated them in. Half rounds down, so half of an odd balance leaves
// the extra coin behind, and half or all of an empty balance resolves to zero
// rather than an error — the handlers report that as a successful transfer of
// nothing.
func resolveAmount(balance int64, amt int, amtOk, isAll, isHalf bool) (int64, error) {
	if amtOk && amt <= 0 {
		return 0, ErrAmountNotPositive
	}
	if !amtOk && !isAll && !isHalf {
		return 0, ErrNoAmountGiven
	}

	switch {
	case isHalf:
		return balance / 2, nil
	case isAll:
		return balance, nil
	default:
		if int64(amt) > balance {
			return 0, ErrInsufficientBalance
		}
		return int64(amt), nil
	}
}
