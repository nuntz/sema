package store

import (
	"fmt"
	"path"
	"strings"
)

func BodyKey(userID, itemID string) string {
	return fmt.Sprintf("bodies/%s/%s.html", userID, itemID)
}

func MediaKey(userID, itemID, extension string) string {
	return fmt.Sprintf("media/%s/%s/lead%s", userID, itemID, extension)
}

func MediaVariantKey(mediaKey string, width int) string {
	extension := path.Ext(mediaKey)
	return fmt.Sprintf("%s-%d%s", strings.TrimSuffix(mediaKey, extension), width, extension)
}

func EmbedMediaKey(userID, itemID string, index int) string {
	return fmt.Sprintf("media/%s/%s/embed-%d.webp", userID, itemID, index)
}

func BodyImageKey(userID, itemID string, index int) string {
	return fmt.Sprintf("media/%s/%s/body-%d.webp", userID, itemID, index)
}

func FaviconKey(feedID string) string {
	return fmt.Sprintf("favicons/%s.png", feedID)
}

func ArchiveBodyKey(userID, itemID string) string {
	return fmt.Sprintf("archive/%s/%s/body.html", userID, itemID)
}

func ArchiveMediaKey(userID, itemID string) string {
	return fmt.Sprintf("archive/%s/%s/lead.webp", userID, itemID)
}
