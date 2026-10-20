//go:build server

package main

import (
	"log"
	"os/exec"

	"github.com/TaterTotterson/Tater-Echo-Firmware/internal/platform"
)

func stopMixer() {
	if platform.Base() == platform.TaterLinux {
		return
	}
	cmd := exec.Command("stop", "mixer")
	if err := cmd.Run(); err != nil {
		log.Printf("stop mixer: %v (continuing)", err)
	}
}
