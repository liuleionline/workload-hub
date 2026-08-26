package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr         string
	DatabasePath string
	BackupDir    string
	BaseURL      string
	CookieSecure bool
	Location     *time.Location
}

func loadConfig() Config {
	loadDotEnv(".env")
	locName := envOr("APP_TIMEZONE", "Asia/Shanghai")
	loc, err := time.LoadLocation(locName)
	if err != nil {
		loc = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	secure, _ := strconv.ParseBool(envOr("APP_COOKIE_SECURE", "false"))
	return Config{
		Addr:         envOr("APP_ADDR", "127.0.0.1:8080"),
		DatabasePath: envOr("APP_DB", "./data/workload.db"),
		BackupDir:    envOr("APP_BACKUP_DIR", "./backup"),
		BaseURL:      strings.TrimRight(envOr("APP_BASE_URL", "http://127.0.0.1:8080"), "/"),
		CookieSecure: secure,
		Location:     loc,
	}
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), "\"'")
		if key != "" {
			if _, exists := os.LookupEnv(key); !exists {
				_ = os.Setenv(key, value)
			}
		}
	}
}
