package service

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

// Image APIs use URL strings, OpenAI image_url objects, and Grok url objects.
// Keep extraction identical for forwarding and the moderation request.
func imageInputReferenceURL(value gjson.Result) string {
	if value.Type == gjson.String {
		return strings.TrimSpace(value.String())
	}
	if !value.IsObject() {
		return ""
	}
	for _, field := range []string{"url", "image_url"} {
		candidate := value.Get(field)
		if candidate.Type == gjson.String && strings.TrimSpace(candidate.String()) != "" {
			return strings.TrimSpace(candidate.String())
		}
		if field == "image_url" && candidate.IsObject() {
			if nested := candidate.Get("url"); nested.Type == gjson.String {
				return strings.TrimSpace(nested.String())
			}
		}
	}
	return ""
}

func parseImageInputReferences(value gjson.Result, field string) ([]string, error) {
	items := []gjson.Result{value}
	if value.IsArray() {
		items = value.Array()
	}
	refs := make([]string, 0, len(items))
	for _, item := range items {
		if item.IsObject() && item.Get("url").Exists() && item.Get("image_url").Exists() {
			return nil, fmt.Errorf("%s must use exactly one image URL field", field)
		}
		if item.IsObject() && item.Get("file_id").Exists() {
			return nil, fmt.Errorf("%s.file_id is not supported (use image_url instead)", field)
		}
		imageURL := imageInputReferenceURL(item)
		if imageURL == "" {
			return nil, fmt.Errorf("%s must contain non-empty image URLs", field)
		}
		refs = append(refs, imageURL)
	}
	return refs, nil
}
