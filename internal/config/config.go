package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func LoadDotEnv() {
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, value)
		}
	}
}

type Config struct {
	TelegramToken   string
	League          string
	UserAgent       string
	CacheTTL        time.Duration
	MinDivineVolume uint64
	LogLevel        string
	DBPath          string
}

func takeEnv(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("env variable was not found: %s", key)
	}
	return value, nil
}

func LoadConfig() (Config, error) {
	LoadDotEnv()

	token, err := takeEnv("TELGRAM_BOT_TOKEN")
	if err != nil {
		return Config{}, err
	}

	league, err := takeEnv("POE_LEAGUE")
	if err != nil {
		league = "Forbidden Rites"
	}

	contact, err := takeEnv("POE_CONTACT")
	if err != nil {
		return Config{}, err
	}
	user := fmt.Sprintf("poe-tg-tracker/0.1.0 (contact: %s)", contact)

	logLevel, err := takeEnv("LOG_LEVEL")
	if err != nil {
		logLevel = "info"
	}

	dbPath, err := takeEnv("DB_PATH")
	if err != nil {
		return Config{}, err
	}

	return Config{
		TelegramToken:   token,
		League:          league,
		UserAgent:       user,
		MinDivineVolume: 50,
		CacheTTL:        10 * time.Minute,
		LogLevel:        logLevel,
		DBPath:          dbPath,
	}, nil
}
