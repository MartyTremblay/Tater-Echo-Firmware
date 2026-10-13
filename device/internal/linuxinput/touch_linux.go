//go:build linux

// Package linuxinput reads Checkers, Crown, and Rook touchscreens directly from evdev.
// Its small evdev reader is derived from TECHO5 (MIT).
package linuxinput

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

const (
	evSyn           = 0
	evKey           = 1
	evAbs           = 3
	synReport       = 0
	synDropped      = 3
	btnTouch        = 0x14a
	absMTSlot       = 0x2f
	absMTPositionX  = 0x35
	absMTPositionY  = 0x36
	absMTTrackingID = 0x39
	absPositionX    = 0x00
	absPositionY    = 0x01
	kernelLongBytes = strconv.IntSize / 8
	inputEventBytes = 2*kernelLongBytes + 8
	inputEventType  = 2 * kernelLongBytes
	inputEventCode  = inputEventType + 2
	inputEventValue = inputEventCode + 2
)

type EventKind uint8

const (
	Down EventKind = iota + 1
	Move
	Up
)

type TouchEvent struct {
	Kind EventKind
	X    int
	Y    int
}

type rawEvent struct {
	typeCode uint16
	code     uint16
	value    int32
}

type position struct {
	x, y         int32
	seenX, seenY bool
}

type absInfo struct {
	Value, Min, Max, Fuzz, Flat, Resolution int32
}

type Device struct {
	file        *os.File
	path        string
	rawWidth    int
	rawHeight   int
	frameWidth  int
	frameHeight int
	protocolA   bool
	closeOnce   sync.Once
}

func Open() (*Device, error) {
	paths, err := filepath.Glob("/dev/input/event*")
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		name, err := os.ReadFile("/sys/class/input/" + filepath.Base(path) + "/device/name")
		if err != nil {
			continue
		}
		inputName := strings.TrimSpace(string(name))
		if inputName != "goodix-ts" && inputName != "fts_ts" && inputName != "mtk-tpd" {
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		device := &Device{
			file: file, path: path, rawWidth: 480, rawHeight: 960,
			frameWidth: 960, frameHeight: 480, protocolA: inputName == "mtk-tpd",
		}
		xAxis, yAxis := uint16(absMTPositionX), uint16(absMTPositionY)
		if device.protocolA {
			device.rawHeight = 480
			device.frameWidth, device.frameHeight = 480, 480
		}
		if device.protocolA {
			if x, errX := device.absolute(xAxis); errX != nil || x.Max <= x.Min {
				xAxis, yAxis = absPositionX, absPositionY
			}
		}
		if x, errX := device.absolute(xAxis); errX == nil && x.Max > x.Min {
			device.rawWidth = int(x.Max-x.Min) + 1
		}
		if y, errY := device.absolute(yAxis); errY == nil && y.Max > y.Min {
			device.rawHeight = int(y.Max-y.Min) + 1
		}
		if !device.protocolA {
			// Both Show panels report portrait coordinates while mounted in a
			// landscape enclosure: Checkers is 480x960 and Crown is 800x1280.
			device.frameWidth, device.frameHeight = device.rawHeight, device.rawWidth
		}
		return device, nil
	}
	return nil, fmt.Errorf("no input device named goodix-ts, fts_ts, or mtk-tpd")
}

func (d *Device) String() string {
	return fmt.Sprintf("%s (%dx%d raw)", d.path, d.rawWidth, d.rawHeight)
}

func (d *Device) Close() error {
	var err error
	d.closeOnce.Do(func() { err = d.file.Close() })
	return err
}

func (d *Device) absolute(code uint16) (absInfo, error) {
	var result absInfo
	request := uintptr(0x80184540 + uint32(code))
	connection, err := d.file.SyscallConn()
	if err != nil {
		return result, err
	}
	var errno syscall.Errno
	if err := connection.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(unsafe.Pointer(&result)))
	}); err != nil {
		return result, err
	}
	if errno != 0 {
		return result, errno
	}
	return result, nil
}

func (d *Device) read() (rawEvent, error) {
	buffer := make([]byte, inputEventBytes)
	if _, err := io.ReadFull(d.file, buffer); err != nil {
		return rawEvent{}, err
	}
	return rawEvent{
		typeCode: binary.LittleEndian.Uint16(buffer[inputEventType:]),
		code:     binary.LittleEndian.Uint16(buffer[inputEventCode:]),
		value:    int32(binary.LittleEndian.Uint32(buffer[inputEventValue:])),
	}, nil
}

func (d *Device) Run(ctx context.Context, events chan<- TouchEvent) error {
	stop := context.AfterFunc(ctx, func() { _ = d.Close() })
	defer stop()
	if d.protocolA {
		return d.runProtocolA(ctx, events)
	}
	slots := map[int]*position{}
	slot, activeSlot := 0, -1
	active, down := false, false
	lastX, lastY := -1, -1
	dropping := false
	for {
		event, err := d.read()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read %s: %w", d.path, err)
		}
		if event.typeCode == evSyn && event.code == synDropped {
			slots = map[int]*position{}
			active, down, activeSlot, dropping = false, false, -1, true
			continue
		}
		if dropping {
			if event.typeCode == evSyn && event.code == synReport {
				dropping = false
			}
			continue
		}
		switch event.typeCode {
		case evAbs:
			if event.code == absMTSlot {
				slot = int(event.value)
				continue
			}
			point := slots[slot]
			if point == nil {
				point = &position{}
				slots[slot] = point
			}
			switch event.code {
			case absMTPositionX:
				point.x, point.seenX = event.value, true
			case absMTPositionY:
				point.y, point.seenY = event.value, true
			case absMTTrackingID:
				if event.value >= 0 && !active {
					active, activeSlot = true, slot
					down = false
					lastX, lastY = -1, -1
				} else if event.value < 0 && active && activeSlot == slot {
					if down {
						x, y := d.frame(point)
						deliver(ctx, events, TouchEvent{Kind: Up, X: x, Y: y})
					}
					delete(slots, slot)
					active, down, activeSlot = false, false, -1
				}
			}
		case evKey:
			if event.code == btnTouch {
				if event.value != 0 {
					// Some fts_ts revisions only publish a tracking ID for the
					// first contact after opening evdev, while BTN_TOUCH continues
					// to delimit every later contact. Treat either signal as the
					// beginning of a contact so the reader does not become a
					// one-shot touchscreen.
					if !active {
						active, activeSlot = true, slot
						down = false
						lastX, lastY = -1, -1
					}
				} else if active {
					if down {
						position := slots[activeSlot]
						x, y := d.frame(position)
						deliver(ctx, events, TouchEvent{Kind: Up, X: x, Y: y})
					}
					delete(slots, activeSlot)
					active, down, activeSlot = false, false, -1
				}
			}
		case evSyn:
			if event.code == synReport && active {
				point := slots[activeSlot]
				if point == nil || !point.seenX || !point.seenY {
					continue
				}
				x, y := d.frame(point)
				if !down {
					down = true
					lastX, lastY = x, y
					deliver(ctx, events, TouchEvent{Kind: Down, X: x, Y: y})
				} else if x != lastX || y != lastY {
					lastX, lastY = x, y
					deliver(ctx, events, TouchEvent{Kind: Move, X: x, Y: y})
				}
			}
		}
	}
}

// Rook's mtk-tpd sends protocol-A positions and BTN_TOUCH transitions, not
// Checkers' slots and tracking IDs. Emit Down at SYN_REPORT so both coordinates
// have arrived before a tap can activate an on-screen control.
func (d *Device) runProtocolA(ctx context.Context, events chan<- TouchEvent) error {
	point := &position{}
	touching, active := false, false
	lastX, lastY := -1, -1
	for {
		event, err := d.read()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read %s: %w", d.path, err)
		}
		switch event.typeCode {
		case evAbs:
			switch event.code {
			case absMTPositionX, absPositionX:
				point.x, point.seenX = event.value, true
			case absMTPositionY, absPositionY:
				point.y, point.seenY = event.value, true
			}
		case evKey:
			if event.code == btnTouch {
				touching = event.value != 0
				if !touching && active {
					deliver(ctx, events, TouchEvent{Kind: Up, X: lastX, Y: lastY})
					active = false
				}
			}
		case evSyn:
			if event.code == synDropped {
				if active {
					deliver(ctx, events, TouchEvent{Kind: Up, X: lastX, Y: lastY})
				}
				touching, active = false, false
				point = &position{}
				continue
			}
			if event.code != synReport || !touching || !point.seenX || !point.seenY {
				continue
			}
			x, y := d.frame(point)
			if !active {
				active = true
				deliver(ctx, events, TouchEvent{Kind: Down, X: x, Y: y})
			} else if x != lastX || y != lastY {
				deliver(ctx, events, TouchEvent{Kind: Move, X: x, Y: y})
			}
			lastX, lastY = x, y
		}
	}
}

func (d *Device) frame(position *position) (int, int) {
	if position == nil || !position.seenX || !position.seenY {
		return 0, 0
	}
	if d.protocolA {
		width, height := d.frameWidth, d.frameHeight
		if width == 0 {
			width = 480
		}
		if height == 0 {
			height = 480
		}
		x := int(position.x) * width / max(d.rawWidth, 1)
		y := int(position.y) * height / max(d.rawHeight, 1)
		return min(max(x, 0), width-1), min(max(y, 0), height-1)
	}
	width, height := d.frameWidth, d.frameHeight
	if width == 0 {
		width = d.rawHeight
	}
	if height == 0 {
		height = d.rawWidth
	}
	x := int(position.y) * width / max(d.rawHeight, 1)
	y := (d.rawWidth - 1 - int(position.x)) * height / max(d.rawWidth, 1)
	return min(max(x, 0), width-1), min(max(y, 0), height-1)
}

func deliver(ctx context.Context, events chan<- TouchEvent, event TouchEvent) {
	if event.Kind == Move {
		select {
		case events <- event:
		case <-ctx.Done():
		default:
		}
		return
	}
	// Down and Up carry the state transition. Never discard either merely
	// because a render has temporarily filled the queue with movement samples.
	select {
	case events <- event:
	case <-ctx.Done():
	}
}
