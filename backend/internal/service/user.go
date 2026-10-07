package service

import (
	"time"

	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID             int64
	Email          string
	Username       string
	Notes          string
	AvatarURL      string
	AvatarSource   string
	AvatarMIME     string
	AvatarByteSize int
	AvatarSHA256   string
	PasswordHash   string
	Role           string
	Balance        float64
	FrozenBalance  float64
	Concurrency    int
	Status         string
	AllowedGroups  []int64
	// BlockedGroups contains public group IDs explicitly denied to this user.
	// It is intentionally separate from AllowedGroups, which grants exclusive
	// group access and preserves its legacy null/empty semantics.
	BlockedGroups []int64
	TokenVersion  int64 // Persisted revocation version, combined with the password fingerprint.
	// TokenVersionResolved indicates TokenVersion already contains the fingerprint-derived
	// value expected in JWT claims and refresh-token state.
	TokenVersionResolved bool
	SignupSource         string
	LastLoginAt          *time.Time
	LastActiveAt         *time.Time
	LastUsedAt           *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
	DeletedAt            *time.Time // 非 nil 表示用户已软删除

	// GroupRates 用户专属分组倍率配置
	// map[groupID]rateMultiplier
	GroupRates map[int64]float64

	// TOTP 双因素认证字段
	TotpSecretEncrypted *string    // AES-256-GCM 加密的 TOTP 密钥
	TotpEnabled         bool       // 是否启用 TOTP
	TotpEnabledAt       *time.Time // TOTP 启用时间

	// 余额不足通知
	BalanceNotifyEnabled       bool
	BalanceNotifyThresholdType string // "fixed" (default) | "percentage"
	BalanceNotifyThreshold     *float64
	BalanceNotifyExtraEmails   []NotifyEmailEntry
	TotalRecharged             float64

	// RPMLimit 用户级每分钟请求数上限（0 = 不限制）。仅在所用分组未设置 rpm_limit
	// 且该 (用户, 分组) 无 rpm_override 时作为全局兜底生效，计数键 rpm:u:{userID}:{min}。
	RPMLimit int

	// UserGroupRPMOverride 来自 auth cache snapshot 的 (user, group) RPM 覆盖值。
	// nil = 该 API Key 对应的 (user, group) 无 override；非 nil 时 checkRPM 直接使用，
	// 避免每请求查 DB。字段不持久化到数据库。
	UserGroupRPMOverride *int

	APIKeys       []APIKey
	Subscriptions []UserSubscription
}

func (u *User) IsAdmin() bool {
	return u.Role == RoleAdmin
}

func (u *User) IsActive() bool {
	return u.Status == StatusActive
}

// IsGroupBlocked reports whether an administrator explicitly denied this
// user's access to the group.
func (u *User) IsGroupBlocked(groupID int64) bool {
	if u == nil || groupID <= 0 {
		return false
	}
	for _, id := range u.BlockedGroups {
		if id == groupID {
			return true
		}
	}
	return false
}

// IsPublicGroupBlocked limits the deny-list semantics to public standard
// groups.  Blocked rows are intentionally ignored when a group is later
// converted to an exclusive or subscription group, so a historical row cannot
// silently revoke a different permission model.
func (u *User) IsPublicGroupBlocked(groupID int64, isExclusive bool, subscriptionType string) bool {
	return !isExclusive && subscriptionType != SubscriptionTypeSubscription && u.IsGroupBlocked(groupID)
}

// CanBindGroup checks whether a user can bind to a given group.
// For standard groups:
//   - Public standard groups (non-exclusive): all users can bind unless blocked
//   - Subscription groups: callers pass the subscription type and handle the
//     subscription entitlement separately
//   - Exclusive groups: only users with the group in AllowedGroups can bind
func (u *User) CanBindGroup(groupID int64, isExclusive bool, subscriptionType ...string) bool {
	subscription := ""
	if len(subscriptionType) > 0 {
		subscription = subscriptionType[0]
	}
	return u.canBindGroup(groupID, isExclusive, subscription)
}

// CanBindGroupWithSubscriptionType applies the same binding rules while also
// distinguishing subscription groups from public standard groups. The
// subscription entitlement itself is checked by the caller.
func (u *User) CanBindGroupWithSubscriptionType(groupID int64, isExclusive bool, subscriptionType string) bool {
	return u.canBindGroup(groupID, isExclusive, subscriptionType)
}

func (u *User) canBindGroup(groupID int64, isExclusive bool, subscriptionType string) bool {
	if u == nil {
		return false
	}
	if u.IsPublicGroupBlocked(groupID, isExclusive, subscriptionType) {
		return false
	}
	// 公开分组（非专属）：所有用户都可以绑定
	if !isExclusive {
		return true
	}
	// 专属分组：需要在 AllowedGroups 中
	for _, id := range u.AllowedGroups {
		if id == groupID {
			return true
		}
	}
	return false
}

func (u *User) SetPassword(password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hash)
	return nil
}

func (u *User) CheckPassword(password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil
}
