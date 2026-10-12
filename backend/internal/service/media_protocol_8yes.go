package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

// References share the workbench's existing request budget. Its JSON upload
// representation already includes base64 overhead, and the UI caps raw files
// below that budget; a separate image-sized cap would reject accepted videos.
const eightYesVideoMaxUploadBytes = VideoWorkbenchMaxRequestBytes

func ValidVideoReferenceURL(raw string) bool {
	if len(raw) > 8192 || raw != strings.TrimSpace(raw) {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.Fragment != "" || validateImageDownloadURL(parsed) != nil {
		return false
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return false
		}
	}
	return true
}

func normalizeEightYesVideoRequestInfo(info GrokMediaRequestInfo) GrokMediaRequestInfo {
	info.DurationSeconds = NormalizeCustomVideoDurationSeconds(info.RawDurationSeconds)
	// 8yes encodes the billable resolution in the exact model identifier.
	info.Resolution = ""
	for _, resolution := range []string{"480p", "720p", "1080p", "2k", "4k"} {
		if strings.HasSuffix(videoBillingModelBasename(info.Model), "-"+resolution) {
			info.Resolution = resolution
			break
		}
	}
	info.GrokResolution = info.Resolution
	info.ResolutionValid = info.Resolution != ""
	return info
}

func normalizeEightYesVideoJSON(body []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		return nil, fmt.Errorf("8yes requires a video request object")
	}
	readString := func(key string) string {
		var value string
		_ = json.Unmarshal(fields[key], &value)
		return strings.TrimSpace(value)
	}
	model := readString("model")
	if model == "" {
		return nil, fmt.Errorf("8yes requires a model")
	}
	readInteger := func(key string) (int, error) {
		raw := fields[key]
		if len(raw) == 0 {
			return 0, nil
		}
		value := string(raw)
		if raw[0] == '"' {
			value = readString(key)
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 86400 {
			return 0, fmt.Errorf("8yes %s must be a positive integer no greater than 86400", key)
		}
		return n, nil
	}
	duration, err := readInteger("duration")
	if err != nil {
		return nil, err
	}
	seconds, err := readInteger("seconds")
	if err != nil {
		return nil, err
	}
	if duration > 0 && seconds > 0 && duration != seconds {
		return nil, fmt.Errorf("conflicting video duration and seconds")
	}
	if duration == 0 {
		duration = NormalizeCustomVideoDurationSeconds(seconds)
	}
	n, err := readInteger("n")
	if err != nil || n > 1 {
		return nil, fmt.Errorf("8yes creates one video per task")
	}
	for _, field := range []string{"resolution", "resolution_name"} {
		if len(fields[field]) == 0 {
			continue
		}
		value := strings.ToLower(readString(field))
		if _, valid := normalizeVideoBillingResolution(value); !valid && value != "auto" && value != "" {
			return nil, fmt.Errorf("invalid video resolution")
		}
		var typed string
		if json.Unmarshal(fields[field], &typed) != nil {
			return nil, fmt.Errorf("invalid video resolution")
		}
	}
	ratio := firstNonEmpty(readString("ratio"), readString("aspect_ratio"))
	if ratio == "" && readString("size") != "" {
		ratio = strings.ReplaceAll(strings.ToLower(readString("size")), "x", ":")
	}
	if ratio == "" || ratio == "auto" {
		ratio = "16:9"
	}
	parts := strings.Split(ratio, ":")
	if len(parts) != 2 {
		return nil, fmt.Errorf("8yes requires ratio 16:9, 9:16 or 1:1")
	}
	w, errW := strconv.Atoi(parts[0])
	h, errH := strconv.Atoi(parts[1])
	if errW != nil || errH != nil || w < 1 || h < 1 || w > 16384 || h > 16384 {
		return nil, fmt.Errorf("invalid video ratio")
	}
	switch {
	case w == h:
		ratio = "1:1"
	case w*9 == h*16:
		ratio = "16:9"
	case w*16 == h*9:
		ratio = "9:16"
	default:
		return nil, fmt.Errorf("8yes requires ratio 16:9, 9:16 or 1:1")
	}

	result := map[string]any{"model": model, "prompt": readString("prompt"), "duration": duration, "ratio": ratio}
	readReferences := func(keys ...string) ([]string, error) {
		var selected []string
		for _, key := range keys {
			raw := fields[key]
			if len(raw) == 0 || string(raw) == "null" {
				continue
			}
			var values []json.RawMessage
			if json.Unmarshal(raw, &values) != nil {
				values = []json.RawMessage{raw}
			}
			var refs []string
			for _, value := range values {
				var ref string
				if json.Unmarshal(value, &ref) != nil {
					var object struct {
						URL      string `json:"url"`
						ImageURL string `json:"image_url"`
					}
					if json.Unmarshal(value, &object) != nil {
						return nil, fmt.Errorf("invalid 8yes %s reference", key)
					}
					ref = firstNonEmpty(object.URL, object.ImageURL)
				}
				ref = strings.TrimSpace(ref)
				if ref == "" {
					return nil, fmt.Errorf("empty 8yes %s reference", key)
				}
				if !strings.HasPrefix(ref, "data:") {
					if !ValidVideoReferenceURL(ref) {
						return nil, fmt.Errorf("8yes references require public HTTP or HTTPS URLs")
					}
				}
				refs = append(refs, ref)
			}
			if len(selected) > 0 && len(refs) > 0 {
				return nil, fmt.Errorf("conflicting 8yes reference fields")
			}
			if len(refs) > 32 {
				return nil, fmt.Errorf("too many 8yes references")
			}
			if len(refs) > 0 {
				selected = refs
			}
		}
		return selected, nil
	}
	images, err := readReferences("referenceImages", "reference_images", "image_refs")
	if err != nil {
		return nil, err
	}
	firstFrame, err := readReferences("image", "input_reference")
	if err != nil {
		return nil, err
	}
	if len(firstFrame) > 1 || (len(firstFrame) > 0 && len(images) > 0) {
		return nil, fmt.Errorf("image and reference images are mutually exclusive")
	}
	if strings.HasPrefix(videoBillingModelBasename(model), "grok-") {
		// The workbench's single generic image represents a locked first frame.
		if len(images) == 1 && len(fields["reference_images"]) == 0 {
			firstFrame, images = images, nil
		}
		if len(images) > 7 || (len(images) > 0 && strings.HasSuffix(strings.ToLower(model), "-1080p")) {
			return nil, fmt.Errorf("8yes Grok supports at most seven reference images, unavailable at 1080p")
		}
		if len(firstFrame) == 1 {
			result["image"] = firstFrame[0]
		} else if len(images) > 0 {
			refs := make([]map[string]string, 0, len(images))
			for _, image := range images {
				refs = append(refs, map[string]string{"url": image})
			}
			result["reference_images"] = refs
		}
	} else {
		if len(firstFrame) > 0 {
			images = firstFrame
		}
		if len(images) > 0 {
			result["referenceImages"] = images
		}
	}
	for _, reference := range []struct {
		name    string
		aliases []string
	}{
		{"referenceVideos", []string{"referenceVideos", "reference_videos", "video_refs"}},
		{"referenceAudios", []string{"referenceAudios", "reference_audios", "audio_refs"}},
	} {
		refs, err := readReferences(reference.aliases...)
		if err != nil {
			return nil, err
		}
		if len(refs) > 0 {
			result[reference.name] = refs
		}
	}
	return json.Marshal(result)
}

// JSON references remain URLs. Browser uploads are decoded into the provider's
// documented multipart fields so base64 is never sent as a remote reference.
func normalizeEightYesVideoRequest(body []byte, contentType string) ([]byte, string, error) {
	if len(body) > VideoWorkbenchMaxRequestBytes {
		return nil, "", fmt.Errorf("8yes video request exceeds upload limit")
	}
	if !gjson.ValidBytes(body) {
		mediaType, params, err := mime.ParseMediaType(contentType)
		if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
			return nil, "", fmt.Errorf("invalid 8yes video request")
		}
		fields := map[string]any{}
		reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, "", fmt.Errorf("invalid 8yes multipart request")
			}
			data, err := readEightYesVideoUpload(part, eightYesVideoMaxUploadBytes)
			_ = part.Close()
			if err != nil {
				return nil, "", err
			}
			name := strings.TrimSuffix(part.FormName(), "[]")
			switch name {
			case "referenceImages", "reference_images", "image_refs", "image", "input_reference", "referenceVideos", "reference_videos", "video_refs", "referenceAudios", "reference_audios", "audio_refs":
				var refs []json.RawMessage
				if part.FileName() != "" {
					value := "data:" + part.Header.Get("Content-Type") + ";base64," + base64.StdEncoding.EncodeToString(data)
					raw, _ := json.Marshal(value)
					refs = []json.RawMessage{raw}
				} else if json.Valid(data) {
					if json.Unmarshal(data, &refs) != nil {
						refs = []json.RawMessage{data}
					}
				} else {
					raw, _ := json.Marshal(string(data))
					refs = []json.RawMessage{raw}
				}
				existing, _ := fields[name].([]json.RawMessage)
				fields[name] = append(existing, refs...)
				continue
			}
			if part.FileName() != "" {
				return nil, "", fmt.Errorf("unsupported 8yes multipart upload field")
			}
			if fields[name] != nil {
				return nil, "", fmt.Errorf("duplicate 8yes multipart field")
			}
			fields[name] = string(data)
		}
		body, err = json.Marshal(fields)
		if err != nil {
			return nil, "", err
		}
	}
	normalized, err := normalizeEightYesVideoJSON(body)
	if err != nil {
		return nil, "", err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(normalized, &fields)
	var uploads []OpenAIImagesUpload
	for _, field := range []string{"referenceImages", "referenceVideos", "referenceAudios", "image", "reference_images"} {
		value := gjson.GetBytes(normalized, field)
		if !value.Exists() {
			continue
		}
		items := value.Array()
		if !value.IsArray() {
			items = []gjson.Result{value}
		}
		urls := make([]any, 0, len(items))
		for _, item := range items {
			ref := item.String()
			if item.IsObject() {
				ref = item.Get("url").String()
			}
			if !strings.HasPrefix(ref, "data:") {
				if field == "reference_images" {
					urls = append(urls, map[string]string{"url": ref})
				} else {
					urls = append(urls, ref)
				}
				continue
			}
			header, encoded, found := strings.Cut(ref, ",")
			mediaType := strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
			family := "image/"
			if field == "referenceVideos" {
				family = "video/"
			}
			if field == "referenceAudios" {
				family = "audio/"
			}
			if !found || !strings.HasSuffix(header, ";base64") || !strings.HasPrefix(mediaType, family) || len(mediaType) > 100 || strings.ContainsAny(mediaType, "\r\n") || len(encoded) > base64.StdEncoding.EncodedLen(eightYesVideoMaxUploadBytes) {
				return nil, "", fmt.Errorf("invalid 8yes %s upload", field)
			}
			data, err := readEightYesVideoUpload(base64.NewDecoder(base64.StdEncoding, strings.NewReader(encoded)), eightYesVideoMaxUploadBytes)
			if err != nil || len(data) == 0 {
				return nil, "", fmt.Errorf("invalid 8yes reference encoding")
			}
			if field == "reference_images" {
				return nil, "", fmt.Errorf("8yes Grok reference_images require public URLs; use image for an uploaded first frame")
			}
			extension := ".bin"
			if extensions, _ := mime.ExtensionsByType(mediaType); len(extensions) > 0 {
				extension = extensions[0]
			}
			uploads = append(uploads, OpenAIImagesUpload{FieldName: field, FileName: fmt.Sprintf("reference-%d%s", len(uploads)+1, extension), ContentType: mediaType, Data: data})
		}
		if len(urls) == 0 {
			delete(fields, field)
		} else if field == "image" {
			fields[field], _ = json.Marshal(urls[0])
		} else {
			fields[field], _ = json.Marshal(urls)
		}
	}
	if len(uploads) == 0 {
		return normalized, "application/json", nil
	}
	var out bytes.Buffer
	writer := multipart.NewWriter(&out)
	for name, raw := range fields {
		if name == "referenceImages" || name == "referenceVideos" || name == "referenceAudios" {
			var urls []string
			if err := json.Unmarshal(raw, &urls); err != nil {
				return nil, "", fmt.Errorf("invalid 8yes multipart reference URLs")
			}
			// Match 8yes's own client: each URL is a separate same-name field,
			// including when files use that field in the same request.
			for _, referenceURL := range urls {
				if err := writer.WriteField(name, referenceURL); err != nil {
					return nil, "", err
				}
			}
			continue
		}
		value := string(raw)
		if len(raw) > 0 && raw[0] == '"' {
			_ = json.Unmarshal(raw, &value)
		}
		if err := writer.WriteField(name, value); err != nil {
			return nil, "", err
		}
	}
	for _, upload := range uploads {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": upload.FieldName, "filename": upload.FileName}))
		header.Set("Content-Type", upload.ContentType)
		part, err := writer.CreatePart(header)
		if err != nil {
			return nil, "", err
		}
		if _, err := part.Write(upload.Data); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return out.Bytes(), writer.FormDataContentType(), nil
}

func readEightYesVideoUpload(reader io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("invalid 8yes reference upload: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("8yes reference exceeds upload limit")
	}
	return data, nil
}
