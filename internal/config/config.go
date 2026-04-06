package config

import (
	"fmt"
	"os"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

type Config struct {
	VK struct {
		GroupToken string `yaml:"group_token"`
		GroupID    int    `yaml:"group_id"`
		AllowedIDs []int  `yaml:"allowed_ids"`
		APIURL     string `yaml:"api_url"`
	} `yaml:"vk"`
	OpenAI struct {
		APIKey  string `yaml:"api_key"`
		BaseURL string `yaml:"base_url"`
		Model   string `yaml:"model"`
		Reason  string `yaml:"reason"`
		Detail  string `yaml:"detail"`
		Tokens  int    `yaml:"tokens"`
		Store   bool   `yaml:"store"`
	} `yaml:"openai"`
	Bot struct {
		Workers            int `yaml:"workers"`
		MaxConcurrentPerID int `yaml:"max_concurrent_per_user"`
	} `yaml:"bot"`
}

func Load(path string, l *logrus.Logger) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err = yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if cfg.Bot.Workers <= 0 {
		l.Warnf("Bot.Workers установлен как %d, переопределение на 4\n", cfg.Bot.Workers)
		cfg.Bot.Workers = 4
	}
	if cfg.Bot.MaxConcurrentPerID <= 0 {
		l.Warnf("Bot.MaxConcurrentPerID установлен как %d, переопределение на 2\n", cfg.Bot.MaxConcurrentPerID)
		cfg.Bot.MaxConcurrentPerID = 2
	}
	if cfg.OpenAI.Model == "" {
		l.Warnf("OpenAI.Model установлен как %s, переопределение на gpt-5.4\n", cfg.OpenAI.Model)
		cfg.OpenAI.Model = "gpt-5.4"
	}
	r := cfg.OpenAI.Reason
	if r != "low" && r != "medium" && r != "high" {
		l.Warnf("OpenAI.Reason установлен как %s, переопределение на high\n", r)
		cfg.OpenAI.Reason = "high"
	}
	d := cfg.OpenAI.Detail
	if d != "auto" && d != "low" && d != "high" && d != "original" {
		l.Warnf("OpenAI.Detail установлен как %s, переопределение на original\n", d)
		cfg.OpenAI.Detail = "original"
	}
	if cfg.OpenAI.Tokens <= 1000 {
		l.Warnf("OpenAI.Tokens установлен как %d, переопределение на 30000\n", cfg.OpenAI.Tokens)
		cfg.OpenAI.Tokens = 30000
	}

	l.Info("Конфиг: ", cfg)

	return &cfg, nil
}
