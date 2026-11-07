package soravideo

import (
	"bytes"
	"encoding/base64"
	"mime/multipart"
	"strings"
	"testing"
)

func TestParseJSONContract(t *testing.T) {
	for _, seconds := range []string{`8`, `"8"`} {
		body := []byte(`{"model":"videos-mini-720p","prompt":"waves","seconds":` + seconds + `,"size":"1280x720","input_reference":{"image_url":"https://cdn.example/frame.png"}}`)
		input, err := Parse(body, "application/json", 4096, func(raw string) bool { return raw == "https://cdn.example/frame.png" })
		if err != nil || input.Model != "videos-mini-720p" || input.Seconds != 8 || input.Size != "1280x720" || input.Image != "https://cdn.example/frame.png" {
			t.Fatalf("parse %s: input=%+v err=%v", seconds, input, err)
		}
	}
}

func TestParseRejectsInvalidPaidFieldsAndReferences(t *testing.T) {
	for _, fields := range []string{
		`"seconds":0,"size":"1280x720"`,
		`"seconds":8.5,"size":"1280x720"`,
		`"seconds":86401,"size":"1280x720"`,
		`"seconds":8,"size":null`,
		`"seconds":8,"size":"1280x720","duration":8`,
		`"seconds":8,"size":"1280x720","input_reference":["https://cdn.example/a.png"]`,
		`"seconds":8,"size":"1280x720","input_reference":"http://127.0.0.1/a.png"`,
		`"seconds":8,"size":"1280x720","input_reference":"data:image/png;base64,aW52YWxpZA=="`,
	} {
		body := []byte(`{"model":"custom-video","prompt":"waves",` + fields + `}`)
		if _, err := Parse(body, "application/json", 4096, nil); err == nil {
			t.Fatalf("accepted invalid request %s", fields)
		}
	}
	if _, err := Parse([]byte(strings.Repeat("x", 65)), "application/json", 64, nil); err == nil {
		t.Fatal("accepted oversized request")
	}
}

func TestParseMultipartImageAndDuplicateRejection(t *testing.T) {
	image, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/pS8AAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	for _, duplicate := range []bool{false, true} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		for name, value := range map[string]string{"model": "custom-video", "prompt": "waves", "seconds": "8", "size": "1280x720"} {
			if err := writer.WriteField(name, value); err != nil {
				t.Fatal(err)
			}
		}
		part, err := writer.CreateFormFile("input_reference", "frame.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(image); err != nil {
			t.Fatal(err)
		}
		if duplicate {
			if err := writer.WriteField("seconds", "4"); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		input, err := Parse(body.Bytes(), writer.FormDataContentType(), 4096, nil)
		if duplicate {
			if err == nil {
				t.Fatal("accepted duplicate seconds")
			}
		} else if err != nil || input.Image != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(image) || input.Seconds != 8 {
			t.Fatalf("multipart input=%+v err=%v", input, err)
		}
	}
}
