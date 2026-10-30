<template>
  <section
    data-testid="profile-overview-hero"
    class="profile-hero"
  >
    <span class="profile-hero-monogram" aria-hidden="true">{{ avatarInitial }}</span>

    <div class="profile-hero-top">
      <div class="profile-hero-avatar">
        <img
          v-if="avatarUrl"
          :src="avatarUrl"
          :alt="displayName"
        >
        <span v-else aria-hidden="true">{{ avatarInitial }}</span>
        <span
          class="profile-hero-presence"
          :class="isActive ? 'is-active' : 'is-disabled'"
          aria-hidden="true"
        ></span>
      </div>

      <div class="profile-hero-identity">
        <div class="profile-hero-name-row">
          <h2 class="profile-hero-name">{{ displayName }}</h2>
          <span class="profile-chip" :class="user?.role === 'admin' ? 'is-role-admin' : 'is-off'">
            {{ user?.role === 'admin' ? t('profile.administrator') : t('profile.user') }}
          </span>
          <span class="profile-chip" :class="isActive ? 'is-on' : 'is-danger'">
            {{ isActive ? t('common.active') : t('common.disabled') }}
          </span>
        </div>
        <p v-if="primaryEmailDisplay" class="profile-hero-email">
          <Icon name="mail" size="sm" class="shrink-0" />
          <span class="truncate">{{ primaryEmailDisplay }}</span>
        </p>
      </div>
    </div>

    <dl class="profile-hero-stats">
      <div data-testid="profile-overview-metric-balance">
        <dt>{{ t('profile.accountBalance') }}</dt>
        <dd
          data-testid="profile-balance-value"
          :class="balanceToneClass(user?.balance)"
        >
          {{ formatCurrency(user?.balance || 0) }}
        </dd>
      </div>
      <div data-testid="profile-overview-metric-concurrency">
        <dt>{{ t('profile.concurrencyLimit') }}</dt>
        <dd>{{ user?.concurrency || 0 }}</dd>
      </div>
      <div data-testid="profile-overview-metric-member-since">
        <dt>{{ t('profile.memberSince') }}</dt>
        <dd>{{ memberSinceLabel }}</dd>
      </div>
    </dl>
  </section>

  <div data-testid="profile-main-column" class="profile-column">
    <section
      data-testid="profile-basics-panel"
      class="profile-panel"
    >
      <header class="profile-panel-head">
        <span class="profile-panel-icon" aria-hidden="true">
          <Icon name="user" size="md" />
        </span>
        <div class="min-w-0">
          <h2 class="profile-panel-title">{{ t('profile.basicsTitle') }}</h2>
          <p class="profile-panel-desc">{{ t('profile.basicsDescription') }}</p>
        </div>
      </header>

      <div class="profile-panel-body profile-basics">
        <div class="profile-box">
          <ProfileAvatarCard
            :user="user"
            embedded
          />
        </div>
        <div class="profile-box">
          <ProfileEditForm
            :initial-username="user?.username || ''"
            embedded
          />
        </div>
      </div>
    </section>

    <section
      data-testid="profile-auth-bindings-panel"
      class="profile-panel"
    >
      <header class="profile-panel-head">
        <span class="profile-panel-icon is-slate" aria-hidden="true">
          <Icon name="link" size="md" />
        </span>
        <div class="min-w-0">
          <h2 class="profile-panel-title">{{ t('profile.authBindings.title') }}</h2>
          <p class="profile-panel-desc">{{ t('profile.authBindings.description') }}</p>
        </div>
      </header>

      <div class="profile-panel-body">
        <ProfileIdentityBindingsSection
          :user="user"
          :linuxdo-enabled="linuxdoEnabled"
          :dingtalk-enabled="dingtalkEnabled"
          :oidc-enabled="oidcEnabled"
          :oidc-provider-name="oidcProviderName"
          :wechat-enabled="wechatEnabled"
          :wechat-open-enabled="wechatOpenEnabled"
          :wechat-mp-enabled="wechatMpEnabled"
          embedded
          compact
        />
      </div>
    </section>
  </div>

  <div data-testid="profile-side-column" class="profile-column">
    <section
      v-if="sourceHints.length"
      class="profile-panel"
    >
      <header class="profile-panel-head">
        <span class="profile-panel-icon is-plum" aria-hidden="true">
          <Icon name="sync" size="md" />
        </span>
        <div class="min-w-0">
          <h2 class="profile-panel-title">{{ t('profile.linkedProfileSources') }}</h2>
          <p class="profile-panel-desc">{{ t('profile.linkedProfileSourcesDescription') }}</p>
        </div>
      </header>

      <ul class="profile-panel-body profile-sources">
        <li
          v-for="hint in sourceHints"
          :key="hint.key"
          class="profile-source"
        >
          <Icon name="link" size="sm" class="shrink-0" />
          <span>{{ hint.text }}</span>
        </li>
      </ul>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import ProfileAvatarCard from '@/components/user/profile/ProfileAvatarCard.vue'
import ProfileEditForm from '@/components/user/profile/ProfileEditForm.vue'
import ProfileIdentityBindingsSection from '@/components/user/profile/ProfileIdentityBindingsSection.vue'
import type { User, UserAuthBindingStatus, UserAuthProvider, UserProfileSourceContext } from '@/types'

const props = withDefaults(defineProps<{
  user: User | null
  linuxdoEnabled?: boolean
  dingtalkEnabled?: boolean
  oidcEnabled?: boolean
  oidcProviderName?: string
  wechatEnabled?: boolean
  wechatOpenEnabled?: boolean
  wechatMpEnabled?: boolean
}>(), {
  linuxdoEnabled: false,
  dingtalkEnabled: false,
  oidcEnabled: false,
  oidcProviderName: 'OIDC',
  wechatEnabled: false,
  wechatOpenEnabled: undefined,
  wechatMpEnabled: undefined,
})

const { t } = useI18n()

function normalizeBindingStatus(binding: boolean | UserAuthBindingStatus | undefined): boolean | null {
  if (typeof binding === 'boolean') {
    return binding
  }
  if (!binding) {
    return null
  }
  if (typeof binding.bound === 'boolean') {
    return binding.bound
  }
  return Boolean(binding.provider_subject || binding.issuer || binding.provider_key)
}

function isEmailBound(user: User | null | undefined): boolean {
  if (typeof user?.email_bound === 'boolean') {
    return user.email_bound
  }

  const nested = user?.auth_bindings?.email ?? user?.identity_bindings?.email
  const normalized = normalizeBindingStatus(nested)
  return normalized ?? false
}

const isActive = computed(() => props.user?.status === 'active')
const avatarUrl = computed(() => props.user?.avatar_url?.trim() || '')
const displayName = computed(() => props.user?.username?.trim() || props.user?.email?.trim() || t('profile.user'))
const primaryEmailDisplay = computed(() => {
  const email = props.user?.email?.trim() || ''
  if (!email) {
    return ''
  }
  if (email.endsWith('.invalid') && !isEmailBound(props.user)) {
    return ''
  }
  return email
})
const avatarInitial = computed(() => Array.from(displayName.value)[0]?.toUpperCase() || 'U')
const memberSinceLabel = computed(() => {
  const raw = props.user?.created_at?.trim()
  if (!raw) {
    return '-'
  }

  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) {
    return '-'
  }

  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: 'short',
  }).format(date)
})

const providerLabels = computed<Record<UserAuthProvider, string>>(() => ({
  email: t('profile.authBindings.providers.email'),
  linuxdo: t('profile.authBindings.providers.linuxdo'),
  dingtalk: t('profile.authBindings.providers.dingtalk'),
  oidc: t('profile.authBindings.providers.oidc', { providerName: props.oidcProviderName }),
  wechat: t('profile.authBindings.providers.wechat'),
  github: 'GitHub',
  google: 'Google'
}))

function formatCurrency(value: number): string {
  return `$${value.toFixed(2)}`
}

function balanceToneClass(value: number | null | undefined): string {
  const amount = Number(value ?? 0)
  if (!Number.isFinite(amount) || amount < 0) return 'balance-tone-negative'
  if (amount <= 10) return 'balance-tone-low'
  if (amount > 100) return 'balance-tone-premium'
  return 'balance-tone-default'
}

function normalizeProvider(value: string): UserAuthProvider | null {
  const normalized = value.trim().toLowerCase()
  if (
    normalized === 'email' ||
    normalized === 'linuxdo' ||
    normalized === 'wechat' ||
    normalized === 'github' ||
    normalized === 'google'
  ) {
    return normalized
  }
  if (normalized === 'oidc' || normalized.startsWith('oidc:') || normalized.startsWith('oidc/')) {
    return 'oidc'
  }
  return null
}

function readObjectString(source: Record<string, unknown>, ...keys: string[]): string {
  for (const key of keys) {
    const value = source[key]
    if (typeof value === 'string' && value.trim()) {
      return value.trim()
    }
  }
  return ''
}

function resolveThirdPartySource(
  rawSource: string | UserProfileSourceContext | null | undefined
): { provider: UserAuthProvider; label: string } | null {
  if (!rawSource) {
    return null
  }

  if (typeof rawSource === 'string') {
    const provider = normalizeProvider(rawSource)
    if (!provider || provider === 'email') {
      return null
    }
    return {
      provider,
      label: providerLabels.value[provider]
    }
  }

  const sourceRecord = rawSource as Record<string, unknown>
  const provider = normalizeProvider(
    readObjectString(sourceRecord, 'provider', 'source', 'provider_type', 'auth_provider')
  )
  if (!provider || provider === 'email') {
    return null
  }

  const explicitLabel = readObjectString(
    sourceRecord,
    'provider_label',
    'label',
    'provider_name',
    'providerName'
  )

  return {
    provider,
    label: explicitLabel || providerLabels.value[provider]
  }
}

const sourceHints = computed(() => {
  const currentUser = props.user
  if (!currentUser) {
    return []
  }

  const hints: Array<{ key: string; text: string }> = []
  const avatarSource = resolveThirdPartySource(
    currentUser.profile_sources?.avatar ?? currentUser.avatar_source
  )
  const usernameSource = resolveThirdPartySource(
    currentUser.profile_sources?.username ??
      currentUser.profile_sources?.display_name ??
      currentUser.profile_sources?.nickname ??
      currentUser.display_name_source ??
      currentUser.username_source ??
      currentUser.nickname_source
  )

  if (avatarSource) {
    hints.push({
      key: 'avatar',
      text: t('profile.authBindings.source.avatar', { providerName: avatarSource.label })
    })
  }

  if (usernameSource) {
    hints.push({
      key: 'username',
      text: t('profile.authBindings.source.username', { providerName: usernameSource.label })
    })
  }

  return hints
})
</script>

<style scoped>
/* ---------- Identity card ---------- */
.profile-hero {
  position: relative;
  isolation: isolate;
  overflow: hidden;
  padding: 1.5rem;
  border-radius: 1.75rem;
  background:
    radial-gradient(26rem 16rem at 0% 0%, rgb(255 231 138 / 0.85), transparent 70%),
    linear-gradient(135deg, #fff3bd 0%, #fff8dc 48%, #fffbe8 100%);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

/* The account's own initial, set large and faint as a monogram. */
.profile-hero-monogram {
  position: absolute;
  z-index: -1;
  top: 50%;
  right: -0.5rem;
  font-size: 13rem;
  font-weight: 800;
  line-height: 1;
  letter-spacing: -0.04em;
  color: rgb(233 184 36 / 0.16);
  transform: translateY(-58%);
  pointer-events: none;
  user-select: none;
}

.profile-hero-top {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 1.125rem;
}

.profile-hero-avatar {
  position: relative;
  display: flex;
  width: 5.5rem;
  height: 5.5rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 1.75rem;
  background: linear-gradient(145deg, #f0c43d, #c99a12);
  font-size: 2rem;
  font-weight: 800;
  color: #fffef8;
  box-shadow:
    0 0 0 4px rgb(255 254 248 / 0.95),
    0 16px 30px -16px rgb(126 86 0 / 0.65);
}

.profile-hero-avatar img {
  width: 100%;
  height: 100%;
  border-radius: inherit;
  object-fit: cover;
}

.profile-hero-presence {
  position: absolute;
  right: -0.25rem;
  bottom: -0.25rem;
  width: 1.125rem;
  height: 1.125rem;
  border-radius: 999px;
  box-shadow: 0 0 0 3px #fffef8;
}

.profile-hero-presence.is-active { background: #4f9a66; }
.profile-hero-presence.is-disabled { background: #d4614a; }

.profile-hero-identity {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.375rem;
}

.profile-hero-name-row {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.375rem 0.5rem;
}

.profile-hero-name {
  min-width: 0;
  font-size: 1.625rem;
  font-weight: 800;
  line-height: 1.2;
  letter-spacing: -0.01em;
  overflow-wrap: anywhere;
  color: #3f3420;
}

.profile-chip.is-role-admin {
  background: #e9b824;
  color: #3f3420;
}

.profile-hero-email {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.375rem;
  font-size: 0.875rem;
  color: #5b5238;
}

.profile-hero-stats {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  margin-top: 1.375rem;
  border-radius: 1.125rem;
  background: rgb(255 254 248 / 0.76);
  box-shadow: inset 0 1px 0 rgb(255 255 255 / 0.9), 0 1px 2px rgb(63 52 32 / 0.05);
}

.profile-hero-stats > div {
  min-width: 0;
  padding: 0.875rem 1.125rem;
}

.profile-hero-stats > div + div {
  box-shadow: inset 1px 0 0 rgb(215 193 106 / 0.35);
}

.profile-hero-stats dt {
  font-size: 0.75rem;
  font-weight: 600;
  color: #6b6045;
}

.profile-hero-stats dd {
  margin-top: 0.25rem;
  overflow: hidden;
  font-size: 1.25rem;
  font-weight: 750;
  font-variant-numeric: tabular-nums;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: #3f3420;
}

/* ---------- Panels ---------- */
.profile-column {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 1rem;
}

.profile-column:empty {
  display: none;
}

.profile-basics {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 0.75rem;
}

.profile-sources {
  gap: 0.5rem;
}

.profile-source {
  display: flex;
  align-items: flex-start;
  gap: 0.625rem;
  padding: 0.75rem 0.875rem;
  border-radius: 1rem;
  background: #fffbe8;
  font-size: 0.8125rem;
  line-height: 1.25rem;
  color: #5b5238;
}

.profile-source > :first-child {
  margin-top: 0.125rem;
  color: #6e5d84;
}

@container profile (min-width: 40rem) {
  .profile-basics {
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  }
}

@media (max-width: 639px) {
  .profile-hero {
    padding: 1.125rem;
    border-radius: 1.5rem;
  }

  .profile-hero-monogram {
    font-size: 9rem;
  }

  .profile-hero-top {
    align-items: flex-start;
    gap: 0.875rem;
  }

  .profile-hero-avatar {
    width: 4rem;
    height: 4rem;
    border-radius: 1.25rem;
    font-size: 1.5rem;
  }

  .profile-hero-name {
    font-size: 1.25rem;
  }

  .profile-hero-email {
    font-size: 0.8125rem;
  }

  .profile-hero-stats {
    margin-top: 1rem;
  }

  .profile-hero-stats > div {
    padding: 0.75rem 0.625rem;
  }

  .profile-hero-stats dt {
    font-size: 0.6875rem;
  }

  .profile-hero-stats dd {
    font-size: 1rem;
  }
}
</style>
