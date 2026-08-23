package store

import "fmt"

func BodyKey(userID, itemID string) string {
	return fmt.Sprintf("bodies/%s/%s.html", userID, itemID)
}

func MediaKey(userID, itemID, extension string) string {
	return fmt.Sprintf("media/%s/%s/lead%s", userID, itemID, extension)
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
