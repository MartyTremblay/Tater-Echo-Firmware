//go:build linux

package screenambient

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSensorFile(t *testing.T, root, relative, value string) {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTCS3400ProbeConfiguresAndReadsAthenaSensor(t *testing.T) {
	root := t.TempDir()
	for relative, value := range map[string]string{
		"iio:device0/name":                          "mt6577-auxadc\n",
		"iio:device1/name":                          "tcs3400\n",
		"iio:device1/in_intensity_integration_time": "0.002780\n",
		"iio:device1/in_intensity_calibscale":       "1\n",
		"iio:device1/calibscale_available":          "1 4 16 64\n",
		"iio:device1/in_intensity_clear_raw":        "10000\n",
		"iio:device1/in_intensity_red_raw":          "4000\n",
		"iio:device1/in_intensity_green_raw":        "4000\n",
		"iio:device1/in_intensity_blue_raw":         "3000\n",
	} {
		writeSensorFile(t, root, relative, value)
	}
	old := iioRoot
	iioRoot = root
	t.Cleanup(func() { iioRoot = old })

	reader, err := openTCS3400()
	if err != nil {
		t.Fatal(err)
	}
	device := filepath.Join(root, "iio:device1")
	for name, want := range map[string]string{
		"in_intensity_integration_time": "0.278000",
		"in_intensity_calibscale":       "16",
	} {
		contents, readErr := os.ReadFile(filepath.Join(device, name))
		if readErr != nil || strings.TrimSpace(string(contents)) != want {
			t.Errorf("%s = %q (%v), want %q", name, strings.TrimSpace(string(contents)), readErr, want)
		}
	}
	if _, ok, err := reader.Sample(); ok || err != nil {
		t.Fatalf("first post-gain sample = ok %t, err %v", ok, err)
	}
	lux, ok, err := reader.Sample()
	if err != nil || !ok || math.Abs(lux-267.2) > .1 {
		t.Fatalf("Athena sensor sample = %.2f, ok %t, err %v; want about 267.2 lux", lux, ok, err)
	}
}

func TestProcessedIIOProbeScalesRookLux(t *testing.T) {
	root := t.TempDir()
	writeSensorFile(t, root, "iio:device0/name", "opt3001\n")
	writeSensorFile(t, root, "iio:device0/in_illuminance_raw", "90\n")
	writeSensorFile(t, root, "iio:device0/in_illuminance_scale", "0.5\n")
	old := iioRoot
	iioRoot = root
	t.Cleanup(func() { iioRoot = old })

	reader, err := openProcessedIIO()
	if err != nil {
		t.Fatal(err)
	}
	lux, ok, err := reader.Sample()
	if err != nil || !ok || lux != 45 {
		t.Fatalf("Rook processed sample = %.2f, ok %t, err %v; want 45 lux", lux, ok, err)
	}
	if reader.Name() != "opt3001" {
		t.Fatalf("sensor name = %q, want opt3001", reader.Name())
	}
}
