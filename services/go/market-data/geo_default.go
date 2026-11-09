package main

import (
	"database/sql"
	"log"
	"os"
	"strings"
)

// seedRestrictedCountries 在 tradfi_settings 从未被管理员改过（updated_by 为空）时，
// 用 TRADFI_RESTRICTED_COUNTRIES（ISO alpha-2，逗号分隔）写受限国家，后台修改始终优先。
// 环境变量未设置则不动，沿用迁移默认（US、属地和 OFAC 全面制裁名单）。
func seedRestrictedCountries(db *sql.DB) {
	v, set := os.LookupEnv("TRADFI_RESTRICTED_COUNTRIES")
	if !set {
		return // keep the migration default (US + territories + OFAC set)
	}
	var cc []string
	for _, c := range strings.Split(v, ",") {
		if c = strings.ToUpper(strings.TrimSpace(c)); len(c) == 2 {
			cc = append(cc, c)
		}
	}
	res, err := db.Exec(`UPDATE tradfi_settings SET restricted_countries=string_to_array(NULLIF($1,''),','), updated_at=now(), updated_by='env:TRADFI_RESTRICTED_COUNTRIES'
		WHERE id=1 AND updated_by=''`, strings.Join(cc, ","))
	if err != nil {
		log.Printf("tradfi restricted countries default: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("tradfi restricted countries initialised from env: %v", cc)
	}
}
