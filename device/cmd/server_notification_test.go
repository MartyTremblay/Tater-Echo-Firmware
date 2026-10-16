package main

import "testing"

func TestShowNotificationCarriesConfiguredTTL(t *testing.T) {
	notification, _, _ := showNotification(map[string]any{
		"id": "door", "created_at": 100.0, "expires_at": 112.0, "ttl_seconds": 12.0,
		"display_label": "Generated Image", "image_fit": "contain",
	})
	if notification.CreatedAtUnixMS != 100_000 || notification.ExpiresAtUnixMS != 112_000 || notification.TTLMS != 12_000 {
		t.Fatalf("notification timing = %+v", notification)
	}
	if notification.Label != "Generated Image" || notification.ImageFit != "contain" {
		t.Fatalf("notification presentation = %+v", notification)
	}
}
