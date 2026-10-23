//go:build linux

package buttons

import "testing"

func TestEVIOCGKeyRequestMatchesLinuxABI(t *testing.T) {
	if got, want := evioCGKey(inputKeyBitmapBytes), uintptr(0x80804518); got != want {
		t.Fatalf("EVIOCGKEY request = %#x, want %#x", got, want)
	}
}
