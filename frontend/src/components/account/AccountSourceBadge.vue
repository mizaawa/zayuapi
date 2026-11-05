<template>
  <span
    v-if="source"
    class="account-source"
    :class="`is-${source.protocol}`"
    :title="source.tooltip"
    data-testid="account-source"
    :data-source="source.protocol"
    :data-provider-source="source.provider ? 'provider' : 'api'"
  >
    <Icon :name="source.icon" size="xs" class="shrink-0" />
    <span class="account-source-label">{{ source.label }}</span>
    <span v-if="source.provider" class="account-source-provider">{{ source.provider }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { Account } from '@/types'
import { accountProviderBinding } from './providerImport'

const props = defineProps<{
  account: Pick<Account, 'type' | 'extra'>
}>()

const { t } = useI18n()

const source = computed(() => {
  if (props.account.type !== 'apikey' && props.account.type !== 'upstream') return null
  const binding = accountProviderBinding(props.account)
  const snapshotProtocol = props.account.extra?.upstream_billing_probe?.protocol
  const detectedProtocol = normalizeProtocol(snapshotProtocol)
  const bindingProtocol = normalizeProtocol(binding?.siteType)
  const protocol = detectedProtocol === 'unknown' ? bindingProtocol : detectedProtocol
  const label = t(`admin.accounts.upstreamBilling.protocol.${protocol}`)
  const details = [
    t('admin.accounts.upstreamBilling.protocolTooltip', { protocol: label }),
    binding ? t('admin.accounts.source.providerTooltip', { provider: binding.providerName || '-' }) : t('admin.accounts.source.apiTooltip'),
    binding?.keyName ? t('admin.accounts.source.providerKey', { key: binding.keyName }) : '',
    binding?.groupName ? t('admin.accounts.source.providerGroup', { group: binding.groupName }) : ''
  ].filter(Boolean)
  const icons = { newapi: 'database', sub2api: 'server', unknown: 'questionCircle' } as const
  return {
    protocol,
    icon: icons[protocol],
    label,
    provider: binding?.providerName || '',
    tooltip: details.join('\n')
  }
})

function normalizeProtocol(value: unknown): 'newapi' | 'sub2api' | 'unknown' {
  if (typeof value !== 'string') return 'unknown'
  const normalized = value.trim().toLowerCase().replace(/[-_\s]/g, '')
  if (normalized === 'newapi') return 'newapi'
  if (normalized === 'sub2api') return 'sub2api'
  return 'unknown'
}
</script>

<style scoped>
.account-source {
  display: inline-flex;
  max-width: 100%;
  align-items: center;
  gap: 0.3125rem;
  overflow: hidden;
  padding: 0.125rem 0.5rem;
  border-radius: 999px;
  font-size: 0.6875rem;
  font-weight: 600;
  line-height: 1.125rem;
  white-space: nowrap;
}

.account-source.is-newapi {
  background: #e0e7ff;
  color: #4338ca;
}

.account-source.is-sub2api {
  background: #dbeafe;
  color: #1d4ed8;
}

.account-source.is-unknown {
  background: #f3f4f6;
  color: #6b7280;
}

.account-source-provider {
  min-width: 0;
  overflow: hidden;
  padding-left: 0.375rem;
  text-overflow: ellipsis;
  font-weight: 700;
  box-shadow: inset 1px 0 0 currentColor;
}

:global(.dark) .account-source.is-newapi { background: rgb(49 46 129 / 0.35); color: #a5b4fc; }
:global(.dark) .account-source.is-sub2api { background: rgb(30 58 138 / 0.35); color: #93c5fd; }
:global(.dark) .account-source.is-unknown { background: #27272a; color: #a1a1aa; }
</style>
