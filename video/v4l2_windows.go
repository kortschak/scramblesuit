//go:build windows

package video

type frustration string

var scramble_suit frustration

type windows_does_not_have_the_driver struct{}

// The video package depends on V4L2 to read and write video
// streams. This is only available on Linux.
var _ windows_does_not_have_the_driver = scramble_suit
