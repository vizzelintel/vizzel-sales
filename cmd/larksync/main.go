// One-off CLI: go run ./cmd/larksync (requires DATABASE_URL + LARK_* env).
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"vizzel-backend/config"
	"vizzel-backend/handlers"
)

func main() {
	_ = godotenv.Load()
	config.InitDB()

	fmt.Println("=== Lark Diagnose ===")
	d := handlers.RunLarkDiagnose()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(d)

	if !d.AuthOK || !d.ConfigOK || !d.FieldsOK || !d.RecordsOK {
		log.Fatal("diagnose failed — fix Lark config before bulk sync")
	}

	fmt.Println("\n=== Bulk Sync ===")
	res, err := handlers.RunBulkLarkSync()
	if err != nil {
		log.Fatal(err)
	}
	_ = enc.Encode(res)
	if len(res.Failed) > 0 {
		os.Exit(1)
	}
}
