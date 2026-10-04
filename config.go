package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	BotToken        string
	StaffChatID     int64
	StaffIDs        map[int64]bool
	DatabasePath    string
	ChallengeTTL    int64
	MaxAttempts     int
	LockDuration    int64
	RateLimitCount  int
	RateLimitWindow int64
}

func loadConfig() (Config, error) {
	token := strings.TrimSpace(os.Getenv("BOT_TOKEN"))
	if token == "" {
		return Config{}, fmt.Errorf("BOT_TOKEN is required")
	}
	staffChatID, err := requiredInt64("STAFF_CHAT_ID")
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		BotToken:        token,
		StaffChatID:     staffChatID,
		StaffIDs:        parseIDs(os.Getenv("STAFF_IDS")),
		DatabasePath:    envOr("DATABASE_PATH", "./data/support.db"),
		ChallengeTTL:    envInt64("CHALLENGE_TTL_SECONDS", 900),
		MaxAttempts:     envInt("CHALLENGE_MAX_ATTEMPTS", 3),
		LockDuration:    envInt64("CHALLENGE_LOCK_SECONDS", 3600),
		RateLimitCount:  envInt("RATE_LIMIT_COUNT", 5),
		RateLimitWindow: envInt64("RATE_LIMIT_WINDOW_SECONDS", 300),
	}
	if len(cfg.StaffIDs) == 0 {
		return Config{}, fmt.Errorf("STAFF_IDS must contain at least one Telegram user id")
	}
	return cfg, nil
}

func requiredInt64(name string) (int64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return parsed, nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func envInt64(name string, fallback int64) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(name)), 10, 64)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func parseIDs(value string) map[int64]bool {
	ids := make(map[int64]bool)
	for _, item := range strings.Split(value, ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(item), 10, 64)
		if err == nil && id != 0 {
			ids[id] = true
		}
	}
	return ids
}
