package taternative

import "testing"

func TestDisplayWeatherCommandReachesScreenHook(t *testing.T) {
	var got map[string]any
	client, err := New(Config{URL: "ws://tater.test", DeviceID: "echo-show"}, Hooks{
		DisplayWeather: func(payload map[string]any) { got = payload },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	client.handle(Envelope{Type: "display.weather", Payload: map[string]any{
		"available":        true,
		"temperature_text": "74°",
		"condition":        "Partly cloudy",
	}})

	if got == nil || got["available"] != true || got["temperature_text"] != "74°" {
		t.Fatalf("display weather hook payload = %#v", got)
	}
}

func TestDisplayClimateCommandReachesScreenHook(t *testing.T) {
	var got map[string]any
	client, err := New(Config{URL: "ws://tater.test", DeviceID: "echo-show"}, Hooks{
		DisplayClimate: func(payload map[string]any) { got = payload },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	client.handle(Envelope{Type: "display.climate", Payload: map[string]any{
		"available": true, "room_name": "Living Room", "target_temperature": 72.0,
	}})

	if got == nil || got["available"] != true || got["room_name"] != "Living Room" || got["target_temperature"] != 72.0 {
		t.Fatalf("display climate hook payload = %#v", got)
	}
}

func TestDisplayNotificationCommandReachesScreenHook(t *testing.T) {
	var got map[string]any
	client, err := New(Config{URL: "ws://tater.test", DeviceID: "echo-show"}, Hooks{
		DisplayNotification: func(payload map[string]any) { got = payload },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	client.handle(Envelope{Type: "display.notification", Payload: map[string]any{
		"id": "event-1", "camera_name": "Front Door", "description": "A package was delivered.",
	}})

	if got == nil || got["id"] != "event-1" || got["camera_name"] != "Front Door" {
		t.Fatalf("display notification hook payload = %#v", got)
	}
}

func TestDisplayLastEventCommandReachesScreenHook(t *testing.T) {
	var got map[string]any
	client, err := New(Config{URL: "ws://tater.test", DeviceID: "echo-show"}, Hooks{
		DisplayLastEvent: func(payload map[string]any) { got = payload },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	client.handle(Envelope{Type: "display.last_event", Payload: map[string]any{
		"id": "event-2", "camera_name": "Driveway", "created_at": 1234.5,
	}})

	if got == nil || got["id"] != "event-2" || got["camera_name"] != "Driveway" {
		t.Fatalf("display last event hook payload = %#v", got)
	}
}
