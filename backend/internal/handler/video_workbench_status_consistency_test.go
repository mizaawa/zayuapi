package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoWorkbenchResponseStateMatchesBillingTerminalStates(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"standard completed", `{"status":"completed"}`, "completed"},
		{"completed whitespace", `{"status":" completed "}`, "completed"},
		{"nested uppercase success", `{"data":{"status":" SUCCESS "}}`, "completed"},
		{"video whitespace done", `{"video":{"status":"\tDONE\n"}}`, "completed"},
		{"state succeeded", `{"state":"succeeded"}`, "completed"},
		{"failed whitespace", `{"status":" failed "}`, "failed"},
		{"nested cancellation", `{"data":{"state":" CANCELLED "}}`, "failed"},
		{"video failure", `{"video":{"status":" failure "}}`, "failed"},
		{"nested failure overrides outer success", `{"status":"completed","data":{"state":" failed "}}`, "failed"},
		{"outer failure overrides nested success", `{"status":"error","data":{"status":"completed"}}`, "failed"},
		{"processing", `{"status":"in_progress"}`, "processing"},
		{"pending", `{"status":"pending"}`, "processing"},
		{"missing status", `{}`, "processing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, videoWorkbenchResponseState([]byte(tc.body)))
		})
	}
}
