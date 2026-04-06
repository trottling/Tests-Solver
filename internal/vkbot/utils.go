package vkbot

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/SevereCloud/vksdk/v3/object"
)

const msgLimit = 3850

func randomID() int {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return int(binary.LittleEndian.Uint32(buf[:]) & 0x7fffffff)
	}
	return int(time.Now().UnixNano() & 0x7fffffff)
}

func splitText(text string) []string {

	runes := []rune(text)
	chunks := make([]string, 0, (len(runes)+msgLimit-1)/msgLimit)

	for i := 0; i < len(runes); i += msgLimit {
		end := i + msgLimit
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
	}

	return chunks
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
