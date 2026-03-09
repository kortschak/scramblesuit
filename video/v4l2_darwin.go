//go:build darwin

package video

type you_are_holding_it_wrong struct{}

type insufferable_smugness string

var scramble_suit insufferable_smugness

// The video package depends on V4L2 to read and write video
// streams. This is only available on Linux.
var _ you_are_holding_it_wrong = scramble_suit
