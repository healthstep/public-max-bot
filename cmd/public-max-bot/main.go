package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/helthtech/public-max-bot/internal/boot"
	"github.com/helthtech/public-max-bot/internal/obs"
	"github.com/porebric/configs"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	defer obs.RecoverAndExit()

	keysReader, err := os.Open("config/configs_keys.yml")
	if err != nil {
		log.Fatalf("open configs_keys.yml: %v", err)
	}
	confReader, err := os.Open("config/configs.yml")
	if err != nil {
		log.Fatalf("open configs.yml: %v", err)
	}
	if err = configs.New().KeysReader(keysReader).YamlConfigs(confReader).Init(ctx); err != nil {
		log.Fatalf("init configs: %v", err)
	}
	obs.Init(configs.Value(ctx, "service_name").String())

	if err = boot.Run(ctx); err != nil {
		log.Fatalf("public-max-bot: %v", err)
	}
}
