package handlers

import (
	"net/http/httptest"
	"testing"
)

func TestParseNotificationDeliveryHistoryFilter(t *testing.T) {
	req := httptest.NewRequest("GET", "/notification-deliveries?organization_id=7&status=retrying&cursor=42&limit=50", nil)
	filter, err := parseNotificationDeliveryHistoryFilter(req)
	if err != nil {
		t.Fatal(err)
	}
	if filter.OrganizationID != 7 || filter.Status != "retrying" || filter.Cursor != 42 || filter.Limit != 50 {
		t.Fatalf("filter = %#v", filter)
	}

	for _, query := range []string{
		"",
		"organization_id=0",
		"organization_id=7&status=unknown",
		"organization_id=7&cursor=0",
		"organization_id=7&limit=0",
		"organization_id=7&limit=101",
	} {
		req := httptest.NewRequest("GET", "/notification-deliveries?"+query, nil)
		if _, err := parseNotificationDeliveryHistoryFilter(req); err == nil {
			t.Errorf("query %q unexpectedly passed", query)
		}
	}
}
