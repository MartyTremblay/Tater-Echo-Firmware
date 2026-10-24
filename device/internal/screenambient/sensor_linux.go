//go:build linux

package screenambient

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	iioRoot         = "/sys/bus/iio/devices"
	inputClassRoot  = "/sys/class/input"
	inputDeviceRoot = "/dev/input"
	alsMiscRoot     = "/sys/class/misc/m_alsps_misc"
)

// Open probes the sensor path appropriate to target. The order is specific so
// an unrelated IIO channel can never be mistaken for room light.
func Open(target string) (Reader, error) {
	target = strings.ToLower(strings.TrimSpace(target))
	var attempts []func() (Reader, error)
	switch target {
	case "athena":
		attempts = []func() (Reader, error){openTCS3400, openProcessedIIO, openVendorALS}
	case "rook":
		attempts = []func() (Reader, error){openProcessedIIO, openVendorALS}
	default: // Checkers and Crown vendor kernels.
		attempts = []func() (Reader, error){openVendorALS, openProcessedIIO}
	}
	var failures []string
	for _, attempt := range attempts {
		reader, err := attempt()
		if err == nil {
			return reader, nil
		}
		failures = append(failures, err.Error())
	}
	return nil, fmt.Errorf("ambient light sensor unavailable: %s", strings.Join(failures, "; "))
}

type processedIIO struct {
	name, valuePath, scalePath string
}

func openProcessedIIO() (Reader, error) {
	direct, _ := filepath.Glob(filepath.Join(iioRoot, "iio:device*", "in_illuminance_input"))
	for _, path := range direct {
		return &processedIIO{name: iioName(filepath.Dir(path)), valuePath: path}, nil
	}
	raw, _ := filepath.Glob(filepath.Join(iioRoot, "iio:device*", "in_illuminance_raw"))
	for _, path := range raw {
		dir := filepath.Dir(path)
		scale := filepath.Join(dir, "in_illuminance_scale")
		if _, err := os.Stat(scale); err == nil {
			return &processedIIO{name: iioName(dir), valuePath: path, scalePath: scale}, nil
		}
	}
	return nil, fmt.Errorf("no processed IIO illuminance channel under %s", iioRoot)
}

func iioName(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "name"))
	if err != nil || strings.TrimSpace(string(data)) == "" {
		return filepath.Base(dir)
	}
	return strings.TrimSpace(string(data))
}

func (p *processedIIO) Name() string { return p.name }
func (p *processedIIO) Close() error { return nil }
func (p *processedIIO) Sample() (float64, bool, error) {
	value, err := readFloat(p.valuePath)
	if err != nil {
		return 0, false, err
	}
	if p.scalePath != "" {
		scale, scaleErr := readFloat(p.scalePath)
		if scaleErr != nil {
			return 0, false, scaleErr
		}
		value *= scale
	}
	return mathMaxZero(value), true, nil
}

type vendorALS struct{ file *os.File }

func openVendorALS() (Reader, error) {
	if _, err := os.Stat(alsMiscRoot); err != nil {
		return nil, fmt.Errorf("vendor ALS control: %w", err)
	}
	if err := os.WriteFile(filepath.Join(alsMiscRoot, "alsdelay"), []byte(strconv.FormatInt((500*time.Millisecond).Nanoseconds(), 10)), 0o644); err != nil {
		return nil, fmt.Errorf("set vendor ALS period: %w", err)
	}
	if err := os.WriteFile(filepath.Join(alsMiscRoot, "alsactive"), []byte("1"), 0o644); err != nil {
		return nil, fmt.Errorf("enable vendor ALS: %w", err)
	}
	events, _ := filepath.Glob(filepath.Join(inputClassRoot, "event*"))
	for _, event := range events {
		data, err := os.ReadFile(filepath.Join(event, "device", "name"))
		if err != nil || strings.TrimSpace(string(data)) != "m_alsps_input" {
			continue
		}
		file, openErr := os.Open(filepath.Join(inputDeviceRoot, filepath.Base(event)))
		if openErr != nil {
			_ = os.WriteFile(filepath.Join(alsMiscRoot, "alsactive"), []byte("0"), 0o644)
			return nil, openErr
		}
		return &vendorALS{file: file}, nil
	}
	_ = os.WriteFile(filepath.Join(alsMiscRoot, "alsactive"), []byte("0"), 0o644)
	return nil, errors.New("input device m_alsps_input not found")
}

func (v *vendorALS) Name() string { return "m_alsps_input" }
func (v *vendorALS) Close() error {
	_ = os.WriteFile(filepath.Join(alsMiscRoot, "alsactive"), []byte("0"), 0o644)
	if v.file == nil {
		return nil
	}
	return v.file.Close()
}

type absInfo struct{ Value, Min, Max, Fuzz, Flat, Resolution int32 }

func (v *vendorALS) Sample() (float64, bool, error) {
	if v.file == nil {
		return 0, false, errors.New("vendor ALS is closed")
	}
	var info absInfo
	connection, err := v.file.SyscallConn()
	if err != nil {
		return 0, false, err
	}
	var errno syscall.Errno
	if err := connection.Control(func(fd uintptr) {
		// EVIOCGABS(ABS_X), whose value is the latest lux reported by the
		// MediaTek hwmsensor driver even when an unchanged event was dropped.
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(0x80184540), uintptr(unsafe.Pointer(&info)))
	}); err != nil {
		return 0, false, err
	}
	if errno != 0 {
		return 0, false, errno
	}
	return mathMaxZero(float64(info.Value)), true, nil
}

type tcs3400 struct {
	dir         string
	gains       []int
	gainIndex   int
	integration float64
	settle      int
}

func openTCS3400() (Reader, error) {
	names, _ := filepath.Glob(filepath.Join(iioRoot, "iio:device*", "name"))
	for _, namePath := range names {
		data, err := os.ReadFile(namePath)
		if err != nil || strings.TrimSpace(string(data)) != "tcs3400" {
			continue
		}
		dir := filepath.Dir(namePath)
		t := &tcs3400{dir: dir}
		if err := t.write("in_intensity_integration_time", "0.278000"); err != nil {
			return nil, err
		}
		if t.integration, err = readFloat(filepath.Join(dir, "in_intensity_integration_time")); err != nil || t.integration <= 0 {
			return nil, fmt.Errorf("read TCS3400 integration time: %v", err)
		}
		available, err := os.ReadFile(filepath.Join(dir, "calibscale_available"))
		if err != nil {
			return nil, err
		}
		for _, field := range strings.Fields(string(available)) {
			if gain, parseErr := strconv.Atoi(field); parseErr == nil && gain > 0 {
				t.gains = append(t.gains, gain)
			}
		}
		if len(t.gains) == 0 {
			return nil, errors.New("TCS3400 has no available gain values")
		}
		if err := t.setGain(len(t.gains) / 2); err != nil {
			return nil, err
		}
		return t, nil
	}
	return nil, fmt.Errorf("no IIO device named tcs3400 under %s", iioRoot)
}

func (t *tcs3400) Name() string { return "tcs3400" }
func (t *tcs3400) Close() error { return nil }
func (t *tcs3400) write(attribute, value string) error {
	if err := os.WriteFile(filepath.Join(t.dir, attribute), []byte(value), 0o644); err != nil {
		return fmt.Errorf("write TCS3400 %s: %w", attribute, err)
	}
	return nil
}
func (t *tcs3400) setGain(index int) error {
	if err := t.write("in_intensity_calibscale", strconv.Itoa(t.gains[index])); err != nil {
		return err
	}
	t.gainIndex, t.settle = index, 1
	return nil
}
func (t *tcs3400) Sample() (float64, bool, error) {
	values := [4]float64{}
	for index, channel := range []string{"clear", "red", "green", "blue"} {
		value, err := readFloat(filepath.Join(t.dir, "in_intensity_"+channel+"_raw"))
		if err != nil {
			return 0, false, err
		}
		values[index] = value
	}
	counts := colorCounts{clear: values[0], red: values[1], green: values[2], blue: values[3]}
	if t.settle > 0 {
		t.settle--
		return 0, false, nil
	}
	if next := nextTCSGain(counts.clear, t.gains, t.gainIndex, tcsMaxCount(t.integration)); next != t.gainIndex {
		if err := t.setGain(next); err != nil {
			return 0, false, err
		}
		return 0, false, nil
	}
	gain := float64(t.gains[t.gainIndex])
	return tcs3400Lux(counts, gain, t.integration, t.gainIndex == len(t.gains)-1), true, nil
}

func readFloat(path string) (float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
}

func mathMaxZero(value float64) float64 {
	if value < 0 {
		return 0
	}
	return value
}
