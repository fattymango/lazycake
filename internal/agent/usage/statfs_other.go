//go:build !linux

package usage

import "errors"

func statfsOf(string) (uint64, uint64, error) {
	return 0, 0, errors.New("not supported on this platform")
}
