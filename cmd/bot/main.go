package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"

	"testsolverbot/internal/access"
	"testsolverbot/internal/config"
	"testsolverbot/internal/openaiagent"
	"testsolverbot/internal/vkbot"

	"github.com/sirupsen/logrus"
)

func main() {
	l := logrus.New()
	l.SetFormatter(&logrus.TextFormatter{
		ForceColors:            true,
		PadLevelText:           false,
		DisableLevelTruncation: true,
	})

	l.Info("Загрузка конфига...")
	cfgPath := flag.String("config", "config.yaml", "path to config")
	flag.Parse()

	cfg, err := config.Load(*cfgPath, l)
	if err != nil {
		l.Fatalf("Не удалось загрузить конфиг: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mw := access.NewMiddleware(cfg.VK.AllowedIDs, cfg.Bot.MaxConcurrentPerID)

	l.Info("Загрузка ИИ агента...")
	oai := openaiagent.New(cfg)

	l.Info("Загрузка VK бота...")
	bot, err := vkbot.New(cfg.VK.GroupToken, cfg.VK.GroupID, cfg.VK.APIURL, mw, oai, l, cfg.Bot.Workers)
	if err != nil {
		l.Fatalf("Ошибка инициализации VK бота: %v", err)
	}

	log.Printf("bot started")
	if err = bot.Run(ctx); err != nil {
		l.Fatalf("Ошибка запуска VK бота: %v", err)
	}
}
