//go:build linux

package buttons

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	inputKeyBitmapBytes = 128

	iocNrBits   = 8
	iocTypeBits = 8
	iocSizeBits = 14

	iocNrShift   = 0
	iocTypeShift = iocNrShift + iocNrBits
	iocSizeShift = iocTypeShift + iocTypeBits
	iocDirShift  = iocSizeShift + iocSizeBits
	iocRead      = 2
)

type linuxKeyStateReader struct {
	file *os.File
}

func evioCGKey(size uintptr) uintptr {
	return uintptr(iocRead<<iocDirShift) |
		uintptr('E'<<iocTypeShift) |
		uintptr(0x18<<iocNrShift) |
		size<<iocSizeShift
}

func newKeyStateReader(path string) (keyStateReader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &linuxKeyStateReader{file: file}, nil
}

func (r *linuxKeyStateReader) Pressed(code uint16) (bool, error) {
	var bitmap [inputKeyBitmapBytes]byte
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		r.file.Fd(),
		evioCGKey(uintptr(len(bitmap))),
		uintptr(unsafe.Pointer(&bitmap[0])),
	)
	if errno != 0 {
		return false, fmt.Errorf("EVIOCGKEY: %w", errno)
	}
	byteIndex := int(code) / 8
	if byteIndex >= len(bitmap) {
		return false, fmt.Errorf("key code %d exceeds state bitmap", code)
	}
	return bitmap[byteIndex]&(1<<uint(code%8)) != 0, nil
}

func (r *linuxKeyStateReader) Close() error {
	return r.file.Close()
}
