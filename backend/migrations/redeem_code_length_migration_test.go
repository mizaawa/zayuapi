package migrations

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent/migrate"
	"github.com/Wei-Shaw/sub2api/ent/redeemcode"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/stretchr/testify/require"
)

func TestRedeemCodeLengthMigration(t *testing.T) {
	content, err := FS.ReadFile("221_expand_redeem_code_length.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ALTER TABLE redeem_codes ALTER COLUMN code TYPE VARCHAR(64)")

	// Existing payment orders already contain 39-character codes. Both the
	// generated database schema and the runtime validator must accept them.
	require.Equal(t, "code", migrate.RedeemCodesColumns[1].Name)
	require.Equal(t, int64(64), migrate.RedeemCodesColumns[1].Size)
	require.NoError(t, redeemcode.CodeValidator("PAY-01234567-89ABCDEF-01234567-89ABCDEF"))
	require.NoError(t, redeemcode.CodeValidator(strings.Repeat("A", 64)))
	require.Error(t, redeemcode.CodeValidator(strings.Repeat("A", 65)))
	require.Error(t, redeemcode.CodeValidator(""))
}
