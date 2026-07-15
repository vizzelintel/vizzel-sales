package handlers

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestIsCompanyListAction(t *testing.T) {
	cases := map[string]bool{
		"action=company_list":       true,
		"action=dealer_list":        true,
		"foo=1&action=company_list": true,
		"action=other":              false,
		"":                          false,
	}
	for in, want := range cases {
		if got := isCompanyListAction(in); got != want {
			t.Fatalf("isCompanyListAction(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestBuildCompanyListFlex(t *testing.T) {
	flex := buildCompanyListFlex([]companyFlexRow{
		{
			Name:      "บริษัท อิกซิสท์ โซลูชัน จำกัด",
			Address:   "สิงห์บุรี",
			TaxID:     "0175569000231",
			CreatedAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			Name:      "Vizzel Intel Access Co., Ltd.",
			Address:   "กรุงเทพฯ",
			TaxID:     "0105567159853",
			CreatedAt: time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC),
		},
	})

	if flex["type"] != "flex" {
		t.Fatalf("type = %v", flex["type"])
	}
	alt, _ := flex["altText"].(string)
	if alt != "รายชื่อบริษัท (2 รายการ)" {
		t.Fatalf("altText = %q", alt)
	}

	contents, ok := flex["contents"].(map[string]interface{})
	if !ok {
		t.Fatal("missing contents")
	}
	body, ok := contents["body"].(map[string]interface{})
	if !ok {
		t.Fatal("missing body")
	}
	items, ok := body["contents"].([]map[string]interface{})
	if !ok || len(items) < 3 {
		t.Fatalf("expected company cards + separator, got %d items", len(items))
	}

	raw, err := json.Marshal(flex)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if strings.Contains(s, "invite") || strings.Contains(s, "DLR-") || strings.Contains(s, "VIZZEL2025") {
		t.Fatalf("flex must not contain invite codes: %s", s)
	}
	if strings.Contains(s, "พนักงาน") {
		t.Fatalf("flex must not show member count: %s", s)
	}
	if strings.Contains(s, "Dealer") || strings.Contains(s, "Vizzel Intel Access Co., Ltd.\",\"size\":\"xs") {
		// company type badge must be removed (company names may still contain "Vizzel")
		if strings.Contains(s, `"text":"Dealer"`) || strings.Contains(s, `"text":"Vizzel"`) {
			t.Fatalf("flex must not show company type badge: %s", s)
		}
	}
}

func TestBuildCompanyListFlexEmpty(t *testing.T) {
	flex := buildCompanyListFlex(nil)
	alt, _ := flex["altText"].(string)
	if alt != "รายชื่อบริษัท (0 รายการ)" {
		t.Fatalf("altText = %q", alt)
	}
}
