//go:build linux

package privacy

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestReadAthenaEventUsesKernelWordWidth(t *testing.T) {
	buffer := make([]byte, athenaInputEventSize)
	binary.LittleEndian.PutUint16(buffer[athenaEventTypeAt:], athenaMuteEventType)
	binary.LittleEndian.PutUint16(buffer[athenaEventCodeAt:], athenaMuteStateCode)
	binary.LittleEndian.PutUint32(buffer[athenaEventValueAt:], 1)
	path := filepath.Join(t.TempDir(), "event")
	if err := os.WriteFile(path, buffer, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	event, err := readAthenaEvent(file)
	if err != nil {
		t.Fatal(err)
	}
	if event.eventType != athenaMuteEventType || event.code != athenaMuteStateCode || event.value != 1 {
		t.Fatalf("parsed event = %#v", event)
	}
}
