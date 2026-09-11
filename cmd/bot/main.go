package main

import (
	"log"

	"github.com/grysha11/poe-tg-tracker/internal/config"
)

func main() {
	cfg, err := config.LoadConfig(); if err != nil {
		log.Fatal("Config was not able to load: %d", err)
	}

	
}