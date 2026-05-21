package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
)

type larkBitableConfig struct {
	AppToken       string
	TableID        string
	AppTokenSource string // "wiki" | "env"
}

// resolveLarkBitableConfig returns app_token + table_id for Bitable API calls.
// When LARK_WIKI_NODE_TOKEN is set (wiki URL …/wiki/{token}?table=…), app_token is
// resolved from the wiki node obj_token (required for Bitable embedded in Wiki).
func resolveLarkBitableConfig(tenantToken string) (larkBitableConfig, error) {
	tableID := strings.TrimSpace(os.Getenv("LARK_TABLE_ID"))
	if tableID == "" {
		return larkBitableConfig{}, fmt.Errorf("LARK_TABLE_ID not set")
	}

	wikiToken := strings.TrimSpace(os.Getenv("LARK_WIKI_NODE_TOKEN"))
	envApp := strings.TrimSpace(os.Getenv("LARK_BASE_APP_TOKEN"))

	if wikiToken != "" {
		app, err := larkResolveWikiBitableAppToken(tenantToken, wikiToken)
		if err != nil {
			if envApp == "" {
				return larkBitableConfig{}, fmt.Errorf("wiki resolve: %w", err)
			}
			log.Printf("[LARK] wiki resolve failed (%v), using LARK_BASE_APP_TOKEN\n", err)
			return larkBitableConfig{
				AppToken: envApp, TableID: tableID, AppTokenSource: "env_fallback",
			}, nil
		}
		return larkBitableConfig{
			AppToken: app, TableID: tableID, AppTokenSource: "wiki",
		}, nil
	}

	if envApp == "" {
		return larkBitableConfig{}, fmt.Errorf("set LARK_BASE_APP_TOKEN or LARK_WIKI_NODE_TOKEN")
	}
	return larkBitableConfig{
		AppToken: envApp, TableID: tableID, AppTokenSource: "env",
	}, nil
}

func larkResolveWikiBitableAppToken(tenantToken, wikiNodeToken string) (string, error) {
	url := fmt.Sprintf("%s/open-apis/wiki/v2/spaces/get_node?token=%s",
		larkBaseURL(), wikiNodeToken)
	rb, err := larkDo("GET", url, tenantToken, nil)
	if err != nil {
		return "", err
	}
	var res struct {
		Data struct {
			Node struct {
				ObjToken string `json:"obj_token"`
				ObjType  string `json:"obj_type"`
				Title    string `json:"title"`
			} `json:"node"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rb, &res); err != nil {
		return "", err
	}
	if res.Data.Node.ObjToken == "" {
		return "", fmt.Errorf("wiki node has empty obj_token")
	}
	if res.Data.Node.ObjType != "" && res.Data.Node.ObjType != "bitable" {
		return "", fmt.Errorf("wiki node obj_type=%q (expected bitable)", res.Data.Node.ObjType)
	}
	log.Printf("[LARK] wiki node title=%q obj_type=%s app_token=%s…\n",
		res.Data.Node.Title, res.Data.Node.ObjType, truncToken(res.Data.Node.ObjToken))
	return res.Data.Node.ObjToken, nil
}

func truncToken(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:8] + "…"
}
