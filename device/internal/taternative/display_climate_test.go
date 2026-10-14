package taternative

import (
	"encoding/json"
	"testing"
)

func TestClimateActionsUseBoundedNativeMessages(t *testing.T) {
	client, err := New(Config{URL: "ws://tater.test", DeviceID: "echo-show"}, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.connected.Store(true)

	if !client.SetClimateTemperature("living_room", 72, "F") {
		t.Fatal("set temperature was not queued")
	}
	var temperature Envelope
	if err := json.Unmarshal((<-client.out).data, &temperature); err != nil {
		t.Fatal(err)
	}
	if temperature.Type != "display.climate.action" || temperature.Payload["action"] != "set_temperature" || temperature.Payload["room_id"] != "living_room" || temperature.Payload["value"] != float64(72) {
		t.Fatalf("temperature envelope = %#v", temperature)
	}

	if !client.SetClimateMode("living_room", "auto") {
		t.Fatal("set mode was not queued")
	}
	var mode Envelope
	if err := json.Unmarshal((<-client.out).data, &mode); err != nil {
		t.Fatal(err)
	}
	if mode.Type != "display.climate.action" || mode.Payload["action"] != "set_hvac_mode" || mode.Payload["mode"] != "auto" {
		t.Fatalf("mode envelope = %#v", mode)
	}
}
