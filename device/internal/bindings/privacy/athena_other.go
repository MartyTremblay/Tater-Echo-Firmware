//go:build !linux

package privacy

import "errors"

// OpenAthena is unavailable off Linux; keeping the stub lets host-side tests
// compile the complete daemon.
func OpenAthena() (*AthenaController, error) {
	return nil, errors.New("athena privacy switch requires Linux evdev")
}
