package vkbot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/SevereCloud/vksdk/v3/api"
	"github.com/SevereCloud/vksdk/v3/api/params"
	"github.com/SevereCloud/vksdk/v3/events"
	"github.com/SevereCloud/vksdk/v3/longpoll-bot"
	"github.com/SevereCloud/vksdk/v3/object"
	"github.com/sirupsen/logrus"

	"testsolverbot/internal/access"
	"testsolverbot/internal/openaiagent"
	"testsolverbot/internal/worker"
)

type Bot struct {
	l          *logrus.Logger
	vk         *api.VK
	lp         *longpoll.LongPoll
	httpClient *http.Client
	middleware *access.Middleware
	oai        *openaiagent.Client
	pool       *worker.Pool
	workers    int
}

func New(groupToken string, groupID int, apiURL string, middleware *access.Middleware, oai *openaiagent.Client, l *logrus.Logger, workers int) (*Bot, error) {
	vk := api.NewVK(groupToken)
	if apiURL != "" {
		vk.MethodURL = apiURL
	}
	client := &http.Client{Timeout: 30 * time.Second}
	vk.Client = client
	lp, err := longpoll.NewLongPoll(vk, groupID)
	if err != nil {
		return nil, fmt.Errorf("longpoll: %w", err)
	}
	lp.Client = client

	pool := worker.New(workers)
	return &Bot{vk: vk, lp: lp, httpClient: client, middleware: middleware, oai: oai, pool: pool, l: l, workers: workers}, nil
}

func (b *Bot) Run(ctx context.Context) error {
	b.pool.Start(ctx, b.workers)
	b.lp.MessageNew(func(eventCtx context.Context, obj events.MessageNewObject) {
		msg := obj.Message
		if msg.PeerID < 0 || msg.FromID <= 0 {
			return
		}

		b.l.Infof("Входящее сообщение id=%d from=%d text=%s attachements=%d", msg.PeerID, msg.FromID, msg.Text, len(msg.Attachments))

		if !b.middleware.IsAllowed(msg.FromID) {
			if _, err := b.sendMessage(msg.PeerID, msg.ID, "❌ Доступ запрещён"); err != nil {
				fmt.Println(err)
			}
			return
		}
		if !b.middleware.Acquire(msg.FromID) {
			if _, err := b.sendMessage(msg.PeerID, msg.ID, "❌ Слишком много запросов одновременно. Дождитесь завершения текущих."); err != nil {
				fmt.Println(err)
			}
			return
		}

		if !b.pool.Submit(eventCtx, func(ctx context.Context) {
			defer b.middleware.Release(msg.FromID)
			b.handleMessage(ctx, msg)
		}) {
			b.middleware.Release(msg.FromID)
			if _, err := b.sendMessage(msg.PeerID, msg.ID, "❌ Очередь задач переполнена. Попробуйте чуть позже."); err != nil {
				fmt.Println(err)
			}
		}
	})

	return b.lp.RunWithContext(ctx)
}

func (b *Bot) handleMessage(ctx context.Context, msg object.MessagesMessage) {
	imageURLs, err := extractImageURLs(msg.Attachments)
	if err != nil {
		if _, err = b.sendMessage(msg.PeerID, msg.ID, err.Error()); err != nil {
			fmt.Printf("❌ Ошибка: %s", err.Error())
		}
		return
	}

	messageID, err := b.sendMessage(msg.PeerID, msg.ID, "⬇️ Загрузка картинки…")
	if err != nil {
		return
	}

	images := make([]openaiagent.ImageInput, 0, len(imageURLs))
	for idx, imageURL := range imageURLs {
		b.editMessage(msg.PeerID, messageID, fmt.Sprintf("⬇️ Загружаю изображение %d из %d…", idx+1, len(imageURLs)))
		image, mimeType, downloadErr := b.downloadImage(ctx, imageURL)
		if downloadErr != nil {
			b.editMessage(msg.PeerID, messageID, fmt.Sprintf("❌ Не удалось скачать изображение %d: %v", idx+1, downloadErr))
			return
		}
		images = append(images, openaiagent.ImageInput{Data: image, MimeType: mimeType})
	}

	b.editMessage(msg.PeerID, messageID, "📝 Распознаю задания…")
	startTime := time.Now()
	done := make(chan struct{})

	go func() {
		ticker := time.NewTicker(time.Second * 4)
		defer ticker.Stop()
		phase := 0
		phases := []string{"📝 Распознаю задания…", "🧠 Решаю…", "✅ Проверяю ответ…"}
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				phase = (phase + 1) % len(phases)
				b.editMessage(msg.PeerID, messageID, fmt.Sprintf("%s (%.0f сек)", phases[phase], time.Since(startTime).Seconds()))
			}
		}
	}()

	result, err := b.oai.SolveImages(ctx, images)
	close(done)

	if err != nil {
		b.editMessage(msg.PeerID, messageID, fmt.Sprintf("❌ Ошибка ИИ: %v", err.Error()))
		return
	}

	res, marshalErr := json.MarshalIndent(result, "", "  ")
	if marshalErr != nil {
		b.editMessage(msg.PeerID, messageID, fmt.Sprintf("❌ Ошибка сериализации ответа: %v", marshalErr.Error()))
		return
	}

	finalText := string(res)
	if strings.TrimSpace(finalText) == "" {
		finalText = "❌ Пустой ответ ИИ"
	}

	parts := splitText(finalText)
	b.editMessage(msg.PeerID, messageID, parts[0])
	for _, p := range parts[1:] {
		_, _ = b.sendMessage(msg.PeerID, msg.ID, p)
	}
}

func (b *Bot) sendMessage(peerID int, replyTo int, text string) (int, error) {
	s := params.NewMessagesSendBuilder()
	s.Message(text)
	s.PeerID(peerID)
	s.RandomID(randomID())
	if replyTo != 0 {
		s.ReplyTo(replyTo)
	}

	id, err := b.vk.MessagesSend(s.Params)
	if err != nil {
		b.l.Errorf("Ошибка отправки сообщения peerID=%d replyTo=%d text=%q: %v", peerID, replyTo, text, err)
		return 0, err
	}

	b.l.Infof("Отправлено сообщение id=%d peerID=%d replyTo=%d text=%q", id, peerID, replyTo, text)
	return id, nil
}

func (b *Bot) editMessage(peerID, messageID int, text string) {
	s := params.NewMessagesEditBuilder()
	s.Message(text)
	s.PeerID(peerID)
	s.MessageID(messageID)

	if _, err := b.vk.MessagesEdit(s.Params); err != nil {
		b.l.Errorf("Ошибка редактирования сообщения messageID=%d peerID=%d text=%s: %v", messageID, peerID, text, err)
	} else {
		b.l.Infof("Отредактировано сообщение messageID=%d peerID=%d text=%s", messageID, peerID, text)
	}
}

func (b *Bot) downloadImage(ctx context.Context, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}

	var resp *http.Response
	var lastErr error

	for range 3 {
		resp, err = b.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		lastErr = nil
	}

	if lastErr != nil || resp == nil {
		return nil, "", lastErr
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 30<<20))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("%s - %s", resp.Status, body[:100])
	}
	mimeType := resp.Header.Get("Content-Type")
	if i := strings.Index(mimeType, ";"); i > -1 {
		mimeType = mimeType[:i]
	}
	if mimeType == "" {
		mimeType = http.DetectContentType(body)
	}
	if !strings.HasPrefix(mimeType, "image/") {
		if exts, _ := mime.ExtensionsByType(mimeType); len(exts) > 0 {
			_ = exts
		}
		if !strings.HasPrefix(http.DetectContentType(body), "image/") {
			return nil, "", fmt.Errorf("неподдерживаемый тип картинки: %s", mimeType)
		}
		mimeType = http.DetectContentType(body)
	}
	return body, mimeType, nil
}
