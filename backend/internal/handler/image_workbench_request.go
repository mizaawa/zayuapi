package handler

import "github.com/tidwall/gjson"

// encoding/json accepts case-insensitive field aliases while the gateway and
// upstream use exact names. Keep the workbench's validated request unambiguous.
func validImageWorkbenchFields(body []byte) bool {
	root := gjson.ParseBytes(body)
	valid := root.IsObject()
	root.ForEach(func(key, value gjson.Result) bool {
		switch key.String() {
		case "model", "prompt", "quality", "size", "n", "stream", "response_format":
		case "images":
			valid = value.IsArray()
			for _, item := range value.Array() {
				if !item.IsObject() {
					valid = false
					break
				}
				item.ForEach(func(field, _ gjson.Result) bool {
					valid = valid && field.String() == "image_url"
					return valid
				})
			}
		default:
			valid = false
		}
		return valid
	})
	return valid
}
