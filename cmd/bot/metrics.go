package main

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	commandsTotal    metric.Int64Counter
	callbacksTotal   metric.Int64Counter
	rejectedTotal    metric.Int64Counter
	getUpdatesErrors metric.Int64Counter
)

func init() {
	meter := otel.Meter("poetracker/bot")
	var err error
	commandsTotal, err = meter.Int64Counter("poetracker.bot.commands",
		metric.WithDescription("Bot commands handled by command and outcome."))
	if err != nil {
		otel.Handle(err)
	}
	callbacksTotal, err = meter.Int64Counter("poetracker.bot.callbacks",
		metric.WithDescription("Inline keyboard callbacks handled by view and outcome."))
	if err != nil {
		otel.Handle(err)
	}
	rejectedTotal, err = meter.Int64Counter("poetracker.bot.rejected",
		metric.WithDescription("Updates rejected because the user is not whitelisted, by kind."))
	if err != nil {
		otel.Handle(err)
	}
	getUpdatesErrors, err = meter.Int64Counter("poetracker.bot.getupdates.errors",
		metric.WithDescription("Failed getUpdates polls."))
	if err != nil {
		otel.Handle(err)
	}
}
