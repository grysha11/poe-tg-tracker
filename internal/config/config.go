package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
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
	TelegramToken string
	League        string
	LogLevel      string
	GatewayAddr   string
	Whitelist     []int64
}

func takeEnv(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("env variable was not found: %s", key)
	}
	return value, nil
}

func takeEnvInt64Slice(key string) ([]int64, error) {
	raw, err := takeEnv(key)
	if err != nil {
		return nil, err
	}

	parts := strings.Split(raw, ",")
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid telegram user id %q in %s: %w", p, key, err)
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("env variable %s did not contain any valid telegram user ids", key)
	}
	return ids, nil
}

func LoadConfig() (Config, error) {
	LoadDotEnv()

	token, err := takeEnv("TELGRAM_BOT_TOKEN")
	if err != nil {
		return Config{}, err
	}

	league := os.Getenv("POE_LEAGUE")

	logLevel, err := takeEnv("LOG_LEVEL")
	if err != nil {
		logLevel = "info"
	}

	gatewayAddr, err := takeEnv("GATEWAY_ADDR")
	if err != nil {
		return Config{}, err
	}

	whitelist, err := takeEnvInt64Slice("WHITELIST")
	if err != nil {
		return Config{}, err
	}

	return Config{
		TelegramToken: token,
		League:        league,
		LogLevel:      logLevel,
		GatewayAddr:   gatewayAddr,
		Whitelist:     whitelist,
	}, nil
}
