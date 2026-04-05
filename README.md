# VK Test Solver Bot (Go)

Бот для VK, который принимает картинку с тестом, отправляет её в OpenAI и отдаёт ответ

## Возможности

- Работа через белосписочный VK
- Приём изображения как `photo` или `doc`
- Вызов OpenAI Chat Completions API через `openai-go`, можно поменять `base_url`
- Настройка параметров запроса к OpenAI - модель, мышление, кол-во токенов и качество обработки картинки
- Middleware:
  - access-list по `user_id`;
  - ограничение количества одновременных задач на пользователя.
- Пул воркеров для одновременной обработки нескольких запросов.
- Ключи/параметры грузятся из YAML-конфига, включая OpenAI `base_url`.

## Запуск

```bash
go mod tidy
go run ./cmd/bot -config config.yaml
```

## Конфиг

См. `config.example.yaml`.

## Промпт

См. `internal/openaiagent/prompt.go`
