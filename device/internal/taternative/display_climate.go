package taternative

// SetClimateTemperature requests an integration-neutral thermostat setpoint
// through Tater. The server validates the room and action against its current
// integration registry before reaching a provider.
func (c *Client) SetClimateTemperature(roomID string, value float64, unit string) bool {
	return c.sendJSON("display.climate.action", "", map[string]any{
		"action": "set_temperature", "room_id": roomID, "value": value, "temperature_unit": unit,
	})
}

// SetClimateMode changes only the normalized HVAC mode advertised by Tater.
func (c *Client) SetClimateMode(roomID, mode string) bool {
	return c.sendJSON("display.climate.action", "", map[string]any{
		"action": "set_hvac_mode", "room_id": roomID, "mode": mode,
	})
}
