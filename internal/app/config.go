package app

import (
	"encoding/json"
	"fmt"
	"os"
)

// Environment values override file values. LLM credentials are deliberately excluded.
func LoadConfig() error {
	path := os.Getenv("CONFIG_FILE")
	if path == "" {
		return nil
	}
	p, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	var c struct {
		InitialAdminKey      string `json:"initial_admin_key"`
		InitialAdminUsername string `json:"initial_admin_username"`
		InitialAPIKey        string `json:"initial_api_key"`
		DatabaseURL          string `json:"database_url"`
		DataDir              string `json:"data_dir"`
		ListenAddr           string `json:"listen_addr"`
		WebDir               string `json:"web_dir"`
		AllowedOrigins       string `json:"allowed_origins"`
		CookieSecure         bool   `json:"cookie_secure"`
	}
	if e = json.Unmarshal(p, &c); e != nil {
		return e
	}
	for k, v := range map[string]string{"INITIAL_ADMIN_KEY": c.InitialAdminKey, "INITIAL_ADMIN_USERNAME": c.InitialAdminUsername, "INITIAL_API_KEY": c.InitialAPIKey, "DATABASE_URL": c.DatabaseURL, "DATA_DIR": c.DataDir, "LISTEN_ADDR": c.ListenAddr, "WEB_DIR": c.WebDir, "ALLOWED_ORIGINS": c.AllowedOrigins, "COOKIE_SECURE": fmt.Sprint(c.CookieSecure)} {
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
	return nil
}
