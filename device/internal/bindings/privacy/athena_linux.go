//go:build linux

package privacy

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

const (
	athenaKernelLongSize = strconv.IntSize / 8
	athenaInputEventSize = 2*athenaKernelLongSize + 8
	athenaEventTypeAt    = 2 * athenaKernelLongSize
	athenaEventCodeAt    = athenaEventTypeAt + 2
	athenaEventValueAt   = athenaEventCodeAt + 2
)

// OpenAthena opens the gpio-keys node used by TECHO5's mainline Athena kernel
// and verifies that its stateful mute switch can be queried.
func OpenAthena() (*AthenaController, error) {
	data, err := os.ReadFile("/proc/bus/input/devices")
	if err != nil {
		return nil, err
	}
	path := athenaInputPathByName(data, "gpio-keys")
	if path == "" {
		return nil, fmt.Errorf("athena gpio-keys input device not found")
	}
	device, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	controller := &AthenaController{
		state: func() (bool, error) {
			return athenaSwitchState(device, athenaMuteStateCode)
		},
		readEvent: func() (athenaEvent, error) {
			return readAthenaEvent(device)
		},
		close: device.Close,
	}
	if _, err := controller.State(); err != nil {
		_ = device.Close()
		return nil, err
	}
	return controller, nil
}

func readAthenaEvent(file *os.File) (athenaEvent, error) {
	buffer := make([]byte, athenaInputEventSize)
	if _, err := io.ReadFull(file, buffer); err != nil {
		return athenaEvent{}, err
	}
	return athenaEvent{
		eventType: binary.LittleEndian.Uint16(buffer[athenaEventTypeAt:]),
		code:      binary.LittleEndian.Uint16(buffer[athenaEventCodeAt:]),
		value:     int32(binary.LittleEndian.Uint32(buffer[athenaEventValueAt:])),
	}, nil
}

// athenaSwitchState is adapted from TECHO5's MIT-licensed evdev switch reader.
// EVIOCGSW returns one bitmap bit per switch code and, unlike an event read,
// reports a latch that was already set before this process started.
func athenaSwitchState(file *os.File, code uint16) (bool, error) {
	bytes := int(code)/8 + 1
	state := make([]byte, bytes)
	request := uintptr(0x8000451b | uint32(bytes)<<16) // _IOR('E', 0x1b, bytes)
	raw, err := file.SyscallConn()
	if err != nil {
		return false, err
	}
	var ioctlErr syscall.Errno
	if err := raw.Control(func(fd uintptr) {
		_, _, ioctlErr = syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(unsafe.Pointer(&state[0])))
	}); err != nil {
		return false, err
	}
	if ioctlErr != 0 {
		return false, fmt.Errorf("EVIOCGSW %#x: %w", code, ioctlErr)
	}
	return state[code/8]&(1<<(code%8)) != 0, nil
}
