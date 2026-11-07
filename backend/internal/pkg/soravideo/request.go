// Package soravideo parses the opt-in Sora video request contract without
// depending on gateway scheduling or persistence. Reference URL policy is
// supplied by the gateway so all media adapters use the same validation.
package soravideo

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
)

type Request struct {
	Model, Prompt, Size, Image string
	Seconds                    int
}

func Parse(body []byte, contentType string, maxBytes int, validReferenceURL func(string) bool) (Request, error) {
	if maxBytes <= 0 || len(body) > maxBytes {
		return Request{}, fmt.Errorf("sora request exceeds the body limit")
	}
	var fields map[string]json.RawMessage
	if mediaType, params, err := mime.ParseMediaType(contentType); err == nil && mediaType == "multipart/form-data" {
		if params["boundary"] == "" {
			return Request{}, fmt.Errorf("invalid Sora multipart boundary")
		}
		fields = map[string]json.RawMessage{}
		reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return Request{}, fmt.Errorf("invalid Sora multipart request")
			}
			name := part.FormName()
			if _, exists := fields[name]; exists || !requestField(name) {
				_ = part.Close()
				return Request{}, fmt.Errorf("duplicate or unsupported Sora field %q", name)
			}
			data, err := io.ReadAll(io.LimitReader(part, int64(maxBytes)+1))
			_ = part.Close()
			if err != nil || len(data) > maxBytes {
				return Request{}, fmt.Errorf("sora upload exceeds the request limit")
			}
			if part.FileName() != "" {
				if name != "input_reference" || len(data) == 0 {
					return Request{}, fmt.Errorf("only input_reference may be an image file")
				}
				claimed, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
				detected := http.DetectContentType(data)
				if (detected != "image/png" && detected != "image/jpeg" && detected != "image/webp") || (claimed != "" && claimed != "application/octet-stream" && claimed != detected) {
					return Request{}, fmt.Errorf("input_reference must be a PNG, JPEG or WebP image")
				}
				data = []byte("data:" + detected + ";base64," + base64.StdEncoding.EncodeToString(data))
			}
			fields[name], _ = json.Marshal(string(data))
		}
	} else if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return Request{}, fmt.Errorf("sora request must be a JSON object or multipart form")
	}
	for name := range fields {
		if !requestField(name) {
			return Request{}, fmt.Errorf("unsupported Sora field %q", name)
		}
	}
	readString := func(name string) (string, error) {
		if len(fields[name]) == 0 {
			return "", nil
		}
		var value string
		if json.Unmarshal(fields[name], &value) != nil {
			return "", fmt.Errorf("sora %s must be a string", name)
		}
		return strings.TrimSpace(value), nil
	}
	var input Request
	var err error
	if input.Model, err = readString("model"); err != nil {
		return input, err
	}
	if input.Prompt, err = readString("prompt"); err != nil {
		return input, err
	}
	if input.Size, err = readString("size"); err != nil {
		return input, err
	}
	if input.Model == "" || input.Prompt == "" || input.Size == "" || len(input.Model) > 256 || strings.ContainsAny(input.Model, "\r\n\x00") || len(input.Prompt) > 32000 {
		return input, fmt.Errorf("sora model and prompt are required")
	}
	if len(fields["seconds"]) == 0 {
		return input, fmt.Errorf("sora seconds is required")
	}
	seconds := strings.TrimSpace(string(fields["seconds"]))
	if fields["seconds"][0] == '"' {
		seconds, err = readString("seconds")
		if err != nil {
			return input, err
		}
	}
	input.Seconds, err = strconv.Atoi(seconds)
	if err != nil || input.Seconds < 1 || input.Seconds > 86400 {
		return input, fmt.Errorf("sora seconds must be a positive whole number no greater than 86400")
	}
	if raw := fields["input_reference"]; len(raw) > 0 && string(raw) != "null" {
		if raw[0] == '"' {
			input.Image, err = readString("input_reference")
		} else {
			var reference map[string]json.RawMessage
			if json.Unmarshal(raw, &reference) != nil || len(reference) != 1 || len(reference["image_url"]) == 0 {
				return input, fmt.Errorf("input_reference must contain one image_url")
			}
			if json.Unmarshal(reference["image_url"], &input.Image) != nil {
				return input, fmt.Errorf("input_reference image_url must be a string")
			}
		}
		if err != nil || input.Image == "" || ((validReferenceURL == nil || !validReferenceURL(input.Image)) && !validImageDataURL(input.Image, maxBytes)) {
			return input, fmt.Errorf("input_reference must be a public image URL or valid image upload")
		}
	}
	return input, nil
}

func requestField(name string) bool {
	switch name {
	case "model", "prompt", "seconds", "size", "input_reference":
		return true
	default:
		return false
	}
}

func validImageDataURL(raw string, maxBytes int) bool {
	header, encoded, ok := strings.Cut(raw, ",")
	if !ok || (header != "data:image/png;base64" && header != "data:image/jpeg;base64" && header != "data:image/webp;base64") || len(encoded) > base64.StdEncoding.EncodedLen(maxBytes) {
		return false
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	return err == nil && len(data) > 0 && http.DetectContentType(data) == strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
}
