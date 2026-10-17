package client

import (
	"log"
	"os"
	"os/exec"
	"strings"
)

// Version is set at build time via ldflags.
var Version = "dev"

// FirmwareTarget is the release target embedded by the build. It is separate
// from runtime board detection: the former selects the OTA artifact while the
// latter proves which hardware is actually beneath it.
var FirmwareTarget = "biscuit"

// idmePath is Amazon's ID Manager, exported by their kernel driver. It holds
// this unit's factory identity. A variable lets tests point it elsewhere.
var idmePath = "/proc/idme/serial"

// GetSerialNo returns this unit's serial. Native device identity is keyed on
// this value, so missing units must not silently share a plausible identifier.
func GetSerialNo() string {
	if serial := serialFromIdme(); serial != "" {
		return serial
	}
	out, err := exec.Command("getprop", "ro.serialno").Output()
	if err == nil {
		if serial := sanitiseSerial(string(out)); serial != "" {
			return serial
		}
	}
	if serial := serialFromCmdline(); serial != "" {
		log.Printf("[identity] Serial came from the kernel cmdline, not idme — "+
			"%q may be truncated; check /proc/idme/serial", serial)
		return serial
	}
	log.Printf("[identity] Warning: no serial from idme, getprop or cmdline: %v", err)
	return "unknown-device"
}

func serialFromIdme() string {
	b, err := os.ReadFile(idmePath)
	if err != nil {
		return ""
	}
	return sanitiseSerial(string(b))
}

// sanitiseSerial trims and validates a serial read from a device file.
func sanitiseSerial(raw string) string {
	if i := strings.IndexByte(raw, 0); i >= 0 {
		raw = raw[:i]
	}
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > 64 {
		return ""
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return ""
		}
	}
	return s
}

// A variable so tests can point it elsewhere, like idmePath.
var cmdlinePath = "/proc/cmdline"

func serialFromCmdline() string {
	b, err := os.ReadFile(cmdlinePath)
	if err != nil {
		return ""
	}
	const key = "androidboot.serialno="
	for _, field := range strings.Fields(string(b)) {
		if strings.HasPrefix(field, key) {
			return sanitiseSerial(strings.TrimPrefix(field, key))
		}
	}
	return ""
}
