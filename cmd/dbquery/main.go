package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	dbPath := "antigateway.db"
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open db: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	fmt.Println("=== PROVIDERS ===")
	rows, err := db.Query("SELECT name, type, weight, enabled, models, default_model FROM providers")
	if err != nil {
		fmt.Fprintf(os.Stderr, "query providers: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()
	for rows.Next() {
		var name, typ, models, defaultModel string
		var weight int
		var enabled int
		rows.Scan(&name, &typ, &weight, &enabled, &models, &defaultModel)
		fmt.Printf("  name=%s type=%s weight=%d enabled=%d models=%s default_model=%s\n",
			name, typ, weight, enabled, models, defaultModel)
	}

	fmt.Println("\n=== KIRO TOKENS (full) ===")
	rows2, err := db.Query("SELECT key, value FROM kv_store WHERE key LIKE 'kiro%'")
	if err != nil {
		fmt.Fprintf(os.Stderr, "query kv: %v\n", err)
		os.Exit(1)
	}
	defer rows2.Close()
	for rows2.Next() {
		var k, v string
		rows2.Scan(&k, &v)
		// Parse to show structure without full token values
		var m map[string]interface{}
		if json.Unmarshal([]byte(v), &m) == nil {
			// Truncate long fields
			if at, ok := m["access_token"].(string); ok && len(at) > 30 {
				m["access_token"] = at[:30] + "..."
			}
			if rt, ok := m["refresh_token"].(string); ok && len(rt) > 30 {
				m["refresh_token"] = rt[:30] + "..."
			}
			pretty, _ := json.MarshalIndent(m, "    ", "  ")
			fmt.Printf("  key=%s\n    %s\n\n", k, string(pretty))
		} else {
			fmt.Printf("  key=%s\n  value=%s\n\n", k, v[:min(len(v), 200)])
		}
	}
}
