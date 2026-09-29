package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/grysha11/poe-tg-tracker/internal/config"
	"github.com/grysha11/poe-tg-tracker/internal/gatewayclient"
	"github.com/grysha11/poe-tg-tracker/internal/telegram"
	"github.com/grysha11/poe-tg-tracker/internal/telemetry"
)

const (
	longPollTimeout = 30
	maxPollBackoff  = 60 * time.Second
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tel, err := telemetry.Setup(ctx, "bot")
	if err != nil {
		fmt.Fprintf(os.Stderr, "telemetry setup failed: %v\n", err)
		os.Exit(1)
	}
	defer tel.ShutdownWithTimeout()
	log := tel.Log

	app := &App{
		bot: telegram.NewBot(cfg.TelegramToken),
		gw:  gatewayclient.New(cfg.GatewayAddr),
		cfg: cfg,
		log: log,
	}

	h := &health{gw: app.gw}
	h.beat()
	mux := telemetry.NewMetricsMux()
	h.register(mux)
	telemetry.ServeMetrics(ctx, log, mux)

	log.Info("starting", "league", cfg.League, "gateway", cfg.GatewayAddr, "whitelisted_users", len(cfg.Whitelist))

	var offset int64
	pollTimeout := 0
	failures := 0
	for {
		h.beat()
		select {
		case <-ctx.Done():
			log.Info("shutting down")
			return
		default:
		}

		updates, err := app.bot.GetUpdates(ctx, offset, pollTimeout)
		if err != nil {
			if ctx.Err() != nil {
				log.Info("shutting down")
				return
			}
			failures++
			wait := pollBackoff(failures, err)
			getUpdatesErrors.Add(ctx, 1)
			log.Error("getUpdates failed", "err", err, "consecutive_failures", failures, "retry_in", wait)
			select {
			case <-ctx.Done():
			case <-time.After(wait):
			}
			continue
		}
		if failures > 0 {
			log.Info("getUpdates recovered", "after_failures", failures)
			failures = 0
		}
		h.pollOK()
		pollTimeout = longPollTimeout

		if len(updates) > 0 {
			log.Debug("updates received", "count", len(updates), "offset", offset)
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			h.beat()

			switch {
			case u.CallbackQuery != nil:
				app.handleCallback(ctx, u.CallbackQuery)
			case u.Message != nil:
				app.handleMessage(ctx, u.Message)
			default:
				log.Debug("ignoring update", "update_id", u.UpdateID)
			}
		}
	}
}

func pollBackoff(failures int, err error) time.Duration {
	wait := min(time.Second<<min(failures-1, 6), maxPollBackoff)
	if apiErr, ok := errors.AsType[*telegram.APIError](err); ok && apiErr.RetryAfter > wait {
		wait = apiErr.RetryAfter
	}
	return wait
}
