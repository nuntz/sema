package store

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Cached HTML uses only generated filenames within this item's media prefix.
// Matching URLs also covers srcset entries and embedded-card thumbnails.
func (s *Store) archiveBody(ctx context.Context, userID, itemID, source, destination string) (bool, error) {
	if s.s3 == nil || s.bucket == "" {
		return false, fmt.Errorf("content storage is not configured")
	}
	body, contentType, err := s.Content(ctx, source)
	if err != nil {
		exists, checkErr := s.ContentExists(ctx, source)
		if checkErr == nil && !exists {
			return false, nil
		}
		return false, err
	}
	prefix := "media/" + userID + "/" + itemID + "/"
	pattern := archiveAssetPattern(s.ContentURL(prefix))
	replacements := map[string]string{}
	for _, original := range pattern.FindAllString(string(body), -1) {
		if _, ok := replacements[original]; ok {
			continue
		}
		filename := strings.TrimPrefix(original, s.ContentURL(prefix))
		target := "archive/" + userID + "/" + itemID + "/" + filename
		copied, err := s.copyContent(ctx, prefix+filename, target)
		if err != nil {
			return false, err
		}
		if !copied {
			return false, fmt.Errorf("archive asset missing: %s", prefix+filename)
		}
		replacements[original] = s.ContentURL(target)
	}
	rewritten := pattern.ReplaceAllStringFunc(string(body), func(value string) string { return replacements[value] })
	if err := s.PutContent(ctx, destination, contentType, []byte(rewritten)); err != nil {
		return false, err
	}
	return true, nil
}

func archiveAssetPattern(prefix string) *regexp.Regexp {
	return regexp.MustCompile(regexp.QuoteMeta(prefix) + "[a-zA-Z0-9_.-]+")
}
