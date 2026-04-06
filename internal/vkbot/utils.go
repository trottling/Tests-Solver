package vkbot

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"html"
	"path/filepath"
	"sort"
	"strings"
	"testsolverbot/internal/openaiagent"
	"time"
	"unicode/utf8"

	"github.com/SevereCloud/vksdk/v3/object"
)

const msgLimit = 4000

func randomID() int {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return int(binary.LittleEndian.Uint32(buf[:]) & 0x7fffffff)
	}
	return int(time.Now().UnixNano() & 0x7fffffff)
}

func formatText(answers *openaiagent.SolveResult) []string {
	if answers == nil || len(answers.Tasks) == 0 {
		return nil
	}

	var chunks []string
	var currChunk strings.Builder

	appendChunk := func(text string) {
		if text == "" {
			return
		}

		// Если текущий чанк пустой, кладём текст без лишних переносов
		if currChunk.Len() == 0 {
			currChunk.WriteString(text)
			return
		}

		candidate := currChunk.String() + "\n\n\n" + text
		if utf8.RuneCountInString(candidate) > msgLimit {
			chunks = append(chunks, currChunk.String())
			currChunk.Reset()
			currChunk.WriteString(text)
			return
		}

		currChunk.WriteString("\n\n\n")
		currChunk.WriteString(text)
	}

	for _, a := range answers.Tasks {
		taskText := buildTaskText(a)

		// Если одно задание само по себе длиннее лимита,
		// режем его на части
		if utf8.RuneCountInString(taskText) > msgLimit {
			parts := splitByRuneLimit(taskText, msgLimit)
			for _, part := range parts {
				appendChunk(part)
			}
			continue
		}

		appendChunk(taskText)
	}

	if currChunk.Len() > 0 {
		chunks = append(chunks, currChunk.String())
	}

	return chunks
}

func buildTaskText(a openaiagent.SolveTask) string {
	var b strings.Builder

	number := html.EscapeString(a.Number)
	status := html.EscapeString(humanizeStatus(a.Status))

	b.WriteString(fmt.Sprintf("📌 Задание: <b>%s</b> - ✏️ <b>%s</b>\n\n", number, status))

	var answerParts []string
	for _, opt := range a.SelectedOptions {
		answerParts = append(answerParts, html.EscapeString(opt))
	}
	if a.AnswerText != "" {
		answerParts = append(answerParts, html.EscapeString(a.AnswerText))
	}

	if len(answerParts) > 0 {
		b.WriteString(fmt.Sprintf("🧾 Ответ: <b>%s</b>\n", strings.Join(answerParts, " | ")))
	} else {
		b.WriteString("🧾 Ответ: <b>не указан</b>\n")
	}

	if a.Explanation != "" {
		b.WriteString(fmt.Sprintf("\n❔ %s\n", html.EscapeString(a.Explanation)))
	}

	if len(a.UnreadableFragments) > 0 {
		b.WriteString("\n")
		for _, fragment := range a.UnreadableFragments {
			b.WriteString(fmt.Sprintf("❗️ %s\n", html.EscapeString(fragment)))
		}
	}

	return strings.TrimSpace(b.String())
}

func splitByRuneLimit(s string, limit int) []string {
	if s == "" {
		return nil
	}

	var parts []string
	var current strings.Builder

	for _, line := range strings.Split(s, "\n") {
		candidate := line
		if current.Len() > 0 {
			candidate = current.String() + "\n" + line
		}

		if utf8.RuneCountInString(candidate) <= limit {
			if current.Len() > 0 {
				current.WriteString("\n")
			}
			current.WriteString(line)
			continue
		}

		if current.Len() > 0 {
			parts = append(parts, current.String())
			current.Reset()
		}

		// Если даже одна строка длиннее лимита, режем грубо по рунам
		if utf8.RuneCountInString(line) > limit {
			runes := []rune(line)
			for len(runes) > limit {
				parts = append(parts, string(runes[:limit]))
				runes = runes[limit:]
			}
			if len(runes) > 0 {
				current.WriteString(string(runes))
			}
			continue
		}

		current.WriteString(line)
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

func humanizeStatus(status string) string {
	switch status {
	case "solved":
		return "решено"
	case "unreadable":
		return "неразборчиво"
	case "partial":
		return "частично"
	default:
		return status
	}
}

func extractImageURLs(attachments []object.MessagesMessageAttachment) ([]string, error) {
	urls := make([]string, 0, len(attachments))
	for _, att := range attachments {
		switch att.Type {
		case object.AttachmentTypePhoto:
			if len(att.Photo.Sizes) == 0 {
				continue
			}
			sizes := att.Photo.Sizes
			sort.Slice(sizes, func(i, j int) bool {
				return sizes[i].Width*sizes[i].Height > sizes[j].Width*sizes[j].Height
			})
			urls = append(urls, sizes[0].URL)
		case object.AttachmentTypeDoc:
			ext := strings.ToLower(filepath.Ext(att.Doc.Title))
			if ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" || ext == ".gif" || ext == ".bmp" || ext == ".heic" {
				urls = append(urls, att.Doc.URL)
			}
		}
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("пришлите хотя бы одно изображение (фото или файл-картинку)")
	}
	return urls, nil
}
