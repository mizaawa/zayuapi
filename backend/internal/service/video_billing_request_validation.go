package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"strconv"
	"strings"
)

// Reject numeric forms that a provider could charge differently from our parser.
func validateVideoBillingFields(body []byte, contentType string) error {
	values := make(map[string]string)
	if strings.HasPrefix(strings.ToLower(contentType), "multipart/") {
		_, params, err := mime.ParseMediaType(contentType)
		if err != nil || params["boundary"] == "" {
			return fmt.Errorf("invalid video multipart request")
		}
		reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("invalid video multipart request")
			}
			name := part.FormName()
			if name == "duration" || name == "seconds" || name == "n" {
				if _, duplicate := values[name]; duplicate || part.FileName() != "" {
					_ = part.Close()
					return fmt.Errorf("video %s must be a unique scalar", name)
				}
				value, err := io.ReadAll(io.LimitReader(part, 65))
				if err != nil || len(value) > 64 {
					_ = part.Close()
					return fmt.Errorf("invalid video %s", name)
				}
				values[name] = string(value)
			}
			if err := part.Close(); err != nil {
				return fmt.Errorf("invalid video multipart request")
			}
		}
	} else {
		var fields map[string]json.RawMessage
		if json.Unmarshal(body, &fields) != nil || fields == nil {
			return fmt.Errorf("invalid video request object")
		}
		for _, name := range []string{"duration", "seconds", "n"} {
			if raw, exists := fields[name]; exists {
				value := string(raw)
				if len(raw) > 0 && raw[0] == '"' {
					if json.Unmarshal(raw, &value) != nil {
						return fmt.Errorf("invalid video %s", name)
					}
				}
				values[name] = value
			}
		}
	}
	parsed := make(map[string]int)
	for name, value := range values {
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < 1 || n > 86400 {
			return fmt.Errorf("video %s must be a positive whole number within its allowed range", name)
		}
		parsed[name] = n
	}
	if duration, seconds := parsed["duration"], parsed["seconds"]; duration > 0 && seconds > 0 && duration != seconds {
		return fmt.Errorf("conflicting video duration and seconds")
	}
	return nil
}
