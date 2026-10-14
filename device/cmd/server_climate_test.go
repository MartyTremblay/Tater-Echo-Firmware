package main

import "testing"

func TestShowClimateDecodesThermostatAndRoomTemperatures(t *testing.T) {
	got := showClimate(map[string]any{
		"available":            true,
		"room_id":              "living_room",
		"room_name":            "Living Room",
		"current_temperature":  71.0,
		"target_temperature":   72.0,
		"temperature_unit":     "F",
		"current_humidity":     42.0,
		"hvac_mode":            "auto",
		"hvac_action":          "heating",
		"available_hvac_modes": []any{"heat", "cool", "auto", "off"},
		"can_set_temperature":  true,
		"can_set_mode":         true,
		"room_temperatures": []any{
			map[string]any{"room_id": "kitchen", "room_name": "Kitchen", "temperature": 73.0, "temperature_unit": "F"},
		},
	})
	if got == nil || !got.Available || got.RoomID != "living_room" || got.TargetTemperature == nil || *got.TargetTemperature != 72 {
		t.Fatalf("climate = %#v", got)
	}
	if got.CurrentHumidity == nil || *got.CurrentHumidity != 42 || got.HVACMode != "auto" || len(got.AvailableHVACModes) != 4 {
		t.Fatalf("thermostat details = %#v", got)
	}
	if len(got.RoomTemperatures) != 1 || got.RoomTemperatures[0].RoomName != "Kitchen" || got.RoomTemperatures[0].Temperature != 73 {
		t.Fatalf("room temperatures = %#v", got.RoomTemperatures)
	}
}

func TestNormalizeWallpaperIntervalMinutesAcceptsOnlyPresets(t *testing.T) {
	for _, value := range []int{0, 60, 360, 720, 1440} {
		if got := normalizeWallpaperIntervalMinutes(value); got != value {
			t.Fatalf("wallpaper interval %d normalized to %d", value, got)
		}
	}
	if got := normalizeWallpaperIntervalMinutes(17); got != 1440 {
		t.Fatalf("invalid wallpaper interval normalized to %d, want 1440", got)
	}
}
