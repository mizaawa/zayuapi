package repository

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type storedTimeWindows []service.TokenPriceWindow

func (w storedTimeWindows) Value() (driver.Value, error) {
	if len(w) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(w)
	return string(b), err
}
func (w *storedTimeWindows) Scan(v any) error {
	b, ok := v.([]byte)
	if !ok {
		return fmt.Errorf("invalid time_windows database type %T", v)
	}
	return json.Unmarshal(b, w)
}
