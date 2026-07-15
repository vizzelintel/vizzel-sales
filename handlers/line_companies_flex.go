package handlers

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"vizzel-backend/config"
)

type companyFlexRow struct {
	Name      string
	Address   string
	TaxID     string
	CreatedAt time.Time
}

func isCompanyListAction(data string) bool {
	data = strings.TrimSpace(data)
	return data == "action=company_list" ||
		data == "action=dealer_list" ||
		strings.Contains(data, "action=company_list") ||
		strings.Contains(data, "action=dealer_list")
}

func fetchAllCompaniesForFlex(ctx context.Context) ([]companyFlexRow, error) {
	if config.DB == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	rows, err := config.DB.Query(ctx, `
		SELECT name, COALESCE(address,''), COALESCE(tax_id,''), created_at
		FROM companies
		ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]companyFlexRow, 0)
	for rows.Next() {
		var row companyFlexRow
		if err := rows.Scan(&row.Name, &row.Address, &row.TaxID, &row.CreatedAt); err != nil {
			continue
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func flexText(text, size, weight, color string, wrap bool) map[string]interface{} {
	m := map[string]interface{}{
		"type":  "text",
		"text":  text,
		"size":  size,
		"color": color,
	}
	if weight != "" {
		m["weight"] = weight
	}
	if wrap {
		m["wrap"] = true
	}
	return m
}

func orDash(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	return s
}

func formatCompanyDate(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	months := []string{"", "ม.ค.", "ก.พ.", "มี.ค.", "เม.ย.", "พ.ค.", "มิ.ย.", "ก.ค.", "ส.ค.", "ก.ย.", "ต.ค.", "พ.ย.", "ธ.ค."}
	return fmt.Sprintf("%d %s %d", t.Day(), months[int(t.Month())], t.Year())
}

func buildCompanyListFlex(companies []companyFlexRow) map[string]interface{} {
	count := len(companies)
	altText := fmt.Sprintf("รายชื่อบริษัท (%d รายการ)", count)

	bodyContents := make([]map[string]interface{}, 0)

	if count == 0 {
		bodyContents = append(bodyContents, flexText("ยังไม่มีรายการบริษัทในระบบ", "md", "", "#666666", true))
	} else {
		for i, co := range companies {
			if i > 0 {
				bodyContents = append(bodyContents, map[string]interface{}{
					"type":  "separator",
					"color": "#E8E8E8",
					"margin": "lg",
				})
			}

			card := map[string]interface{}{
				"type":    "box",
				"layout":  "vertical",
				"spacing": "sm",
				"contents": []map[string]interface{}{
					flexText(co.Name, "md", "bold", "#111111", true),
					flexText("📍 "+orDash(co.Address), "sm", "", "#444444", true),
					flexText("เลขประจำตัวผู้เสียภาษี: "+orDash(co.TaxID), "xs", "", "#888888", true),
					flexText("สร้างเมื่อ "+formatCompanyDate(co.CreatedAt), "xs", "", "#AAAAAA", false),
				},
			}
			bodyContents = append(bodyContents, card)
		}
	}

	return map[string]interface{}{
		"type":     "flex",
		"altText":  altText,
		"contents": map[string]interface{}{
			"type":   "bubble",
			"size":   "mega",
			"header": map[string]interface{}{
				"type":            "box",
				"layout":          "vertical",
				"backgroundColor": "#7B68C8",
				"paddingAll":      "16px",
				"contents": []map[string]interface{}{
					flexText("รายชื่อบริษัท", "lg", "bold", "#FFFFFF", false),
					flexText(fmt.Sprintf("%d รายการ", count), "sm", "", "#E8E0FF", false),
				},
			},
			"body": map[string]interface{}{
				"type":     "box",
				"layout":   "vertical",
				"spacing":  "md",
				"paddingAll": "16px",
				"contents": bodyContents,
			},
		},
	}
}

func replyCompanyListFlex(replyToken string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	companies, err := fetchAllCompaniesForFlex(ctx)
	if err != nil {
		log.Printf("LINE company list: query failed: %v", err)
		return
	}

	msg := buildCompanyListFlex(companies)
	if err := lineReply(replyToken, []map[string]interface{}{msg}); err != nil {
		log.Printf("LINE company list: reply failed: %v", err)
	}
}
