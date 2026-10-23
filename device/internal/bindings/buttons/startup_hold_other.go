//go:build !linux

package buttons

import "fmt"

func newKeyStateReader(path string) (keyStateReader, error) {
	return nil, fmt.Errorf("current key-state reads are unavailable on this platform")
}
