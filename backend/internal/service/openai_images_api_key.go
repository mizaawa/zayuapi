package service

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/tidwall/gjson"
)

const openAIImagesMaxInputImageBytes = 50 << 20

// Inline workbench references become standard Images API file uploads. Requests
// with remote references or provider-specific structures retain their JSON wire
// format because converting those could discard provider semantics.
func normalizeOpenAIImagesAPIKeyEditBody(body []byte, contentType string) ([]byte, string, error) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil && strings.EqualFold(mediaType, "multipart/form-data") {
		return body, contentType, nil
	}
	root := gjson.ParseBytes(body)
	if !root.IsObject() {
		return body, contentType, nil
	}

	type formField struct{ name, value string }
	var fields []formField
	var images []string
	var mask string
	convertible := true
	seen := make(map[string]bool)
	root.ForEach(func(key, value gjson.Result) bool {
		name := key.String()
		if seen[name] {
			convertible = false
			return false
		}
		seen[name] = true
		switch name {
		case "images", "image", "image_urls", "image_url":
			refs, ok := inlineOpenAIImagesReferences(value)
			if !ok || len(refs) == 0 {
				convertible = false
				return false
			}
			images = append(images, refs...)
		case "mask":
			refs, ok := inlineOpenAIImagesReferences(value)
			if !ok || value.IsArray() || len(refs) != 1 {
				convertible = false
				return false
			}
			mask = refs[0]
		default:
			switch value.Type {
			case gjson.String:
				fields = append(fields, formField{name, value.String()})
			case gjson.Number, gjson.True, gjson.False:
				fields = append(fields, formField{name, value.Raw})
			default:
				convertible = false
				return false
			}
		}
		return true
	})
	if !convertible || len(images) == 0 {
		return body, contentType, nil
	}

	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for _, field := range fields {
		if err := writer.WriteField(field.name, field.value); err != nil {
			return nil, "", fmt.Errorf("encode image field %s: %w", field.name, err)
		}
	}
	for index, imageURL := range images {
		if err := writeInlineOpenAIImagePart(writer, "image[]", fmt.Sprintf("image-%d", index+1), imageURL); err != nil {
			return nil, "", fmt.Errorf("encode input image %d: %w", index+1, err)
		}
	}
	if mask != "" {
		if err := writeInlineOpenAIImagePart(writer, "mask", "mask", mask); err != nil {
			return nil, "", fmt.Errorf("encode image mask: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("finalize image edit multipart: %w", err)
	}
	return buffer.Bytes(), writer.FormDataContentType(), nil
}

func inlineOpenAIImagesReferences(value gjson.Result) ([]string, bool) {
	items := []gjson.Result{value}
	if value.IsArray() {
		items = value.Array()
	}
	refs := make([]string, 0, len(items))
	for _, item := range items {
		if item.IsObject() {
			name, member, ok := singleInlineOpenAIImageProperty(item)
			if !ok {
				return nil, false
			}
			item = member
			if name == "image_url" {
				if item.IsObject() {
					name, member, ok = singleInlineOpenAIImageProperty(item)
					if !ok || name != "url" {
						return nil, false
					}
					item = member
				}
			} else if name != "url" {
				return nil, false
			}
		}
		if item.Type != gjson.String {
			return nil, false
		}
		reference := strings.TrimSpace(item.String())
		if len(reference) < 5 || !strings.EqualFold(reference[:5], "data:") {
			return nil, false
		}
		refs = append(refs, reference)
	}
	return refs, true
}

func singleInlineOpenAIImageProperty(value gjson.Result) (string, gjson.Result, bool) {
	var name string
	var member gjson.Result
	count := 0
	value.ForEach(func(key, value gjson.Result) bool {
		count++
		name, member = key.String(), value
		return count < 2
	})
	return name, member, count == 1
}

func writeInlineOpenAIImagePart(writer *multipart.Writer, field, fileName, dataURL string) error {
	data, contentType, err := decodeInlineOpenAIImage(dataURL)
	if err != nil {
		return err
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
		"name": field, "filename": fileName + extensionForContentType(contentType),
	}))
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = part.Write(data)
	return err
}

func decodeInlineOpenAIImage(dataURL string) ([]byte, string, error) {
	header, payload, ok := strings.Cut(dataURL, ",")
	if !ok || len(header) < len("data:;base64") || !strings.EqualFold(header[:5], "data:") || !strings.EqualFold(header[len(header)-len(";base64"):], ";base64") {
		return nil, "", fmt.Errorf("image must be a base64 data URL")
	}
	contentType, _, err := mime.ParseMediaType(header[5 : len(header)-len(";base64")])
	if err != nil {
		return nil, "", fmt.Errorf("invalid image data URL media type")
	}
	switch contentType {
	case "image/png", "image/jpeg", "image/webp":
	default:
		return nil, "", fmt.Errorf("image data URL must contain PNG, JPEG or WebP")
	}
	encodedSize := len(payload) - strings.Count(payload, "\r") - strings.Count(payload, "\n")
	decodedSize := base64.StdEncoding.DecodedLen(encodedSize)
	padding := strings.TrimRight(payload, "\r\n")
	if strings.HasSuffix(padding, "=") {
		decodedSize--
		if strings.HasSuffix(padding, "==") {
			decodedSize--
		}
	}
	if decodedSize > openAIImagesMaxInputImageBytes {
		return nil, "", fmt.Errorf("image data URL exceeds %d bytes", openAIImagesMaxInputImageBytes)
	}
	decoder := base64.NewDecoder(base64.StdEncoding.Strict(), strings.NewReader(payload))
	data, err := io.ReadAll(io.LimitReader(decoder, openAIImagesMaxInputImageBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("invalid image data URL base64 payload")
	}
	if len(data) > openAIImagesMaxInputImageBytes {
		return nil, "", fmt.Errorf("image data URL exceeds %d bytes", openAIImagesMaxInputImageBytes)
	}
	if http.DetectContentType(data) != contentType {
		return nil, "", fmt.Errorf("image data URL bytes do not match the declared media type")
	}
	return data, contentType, nil
}
