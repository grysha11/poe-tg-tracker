package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/grysha11/poe-tg-tracker/internal/config"
	"github.com/grysha11/poe-tg-tracker/internal/gatewayclient"
	"github.com/grysha11/poe-tg-tracker/internal/logger"
	"github.com/grysha11/poe-tg-tracker/internal/telegram"
)

type App struct {
	bot *telegram.Bot
	gw  *gatewayclient.Client
	cfg config.Config
	log *slog.Logger
}

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config load failed: %v\n", err)
		os.Exit(1)
	}

	log := logger.New(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := &App{
		bot: telegram.NewBot(cfg.TelegramToken),
		gw:  gatewayclient.New(cfg.GatewayAddr),
		cfg: cfg,
		log: log,
	}

	log.Info("starting", "league", cfg.League, "gateway", cfg.GatewayAddr)

	var offset int64
	for {
		select {
		case <-ctx.Done():
			log.Info("shutting down")
			return
		default:
		}

		updates, err := app.bot.GetUpdates(ctx, offset, 30)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Error("getUpdates failed", "err", err)
			time.Sleep(5 * time.Second)
			continue
		}

		for _, u := range updates {
			offset = u.UpdateID + 1

			switch {
			case u.CallbackQuery != nil:
				app.handleCallback(ctx, u.CallbackQuery)
			case u.Message != nil:
				app.handleMessage(ctx, u.Message)
			}
		}
	}
}
