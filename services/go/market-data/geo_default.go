package main

import (
	"database/sql"
	"log"
	"os"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// seedRestrictedCountries applies TRADFI_RESTRICTED_COUNTRIES (ISO alpha-2,
// comma separated) to tradfi_settings only while the row has never been
// edited by an admin (updated_by=”), so admin changes always win. On
// mainnet the default is "US" (owner decision); elsewhere unset = keep the
// migration default.
func seedRestrictedCountries(db *sql.DB) {
	v, set := os.LookupEnv("TRADFI_RESTRICTED_COUNTRIES")
	if !set {
		if !common.IsMainnet() {
			return
		}
		v = "US"
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
