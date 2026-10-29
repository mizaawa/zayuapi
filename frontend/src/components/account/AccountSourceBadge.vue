<template>
  <span
    v-if="source"
    class="account-source"
    :class="source.kind === 'provider' ? 'is-provider' : 'is-api'"
    :title="source.tooltip"
    data-testid="account-source"
    :data-source="source.kind"
  >
    <Icon :name="source.kind === 'provider' ? 'server' : 'key'" size="xs" class="shrink-0" />
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

// Where an API-key account's credentials came from: typed in by an admin, or
// read from a site in Upstream Management ("从服务商导入").
const props = defineProps<{
  account: Pick<Account, 'type' | 'extra'>
}>()

const { t } = useI18n()

const source = computed(() => {
  const binding = accountProviderBinding(props.account)
  if (binding) {
    const details = [
      t('admin.accounts.source.providerTooltip', { provider: binding.providerName || '-' }),
      binding.keyName ? t('admin.accounts.source.providerKey', { key: binding.keyName }) : '',
      binding.groupName ? t('admin.accounts.source.providerGroup', { group: binding.groupName }) : ''
    ].filter(Boolean)
    return {
      kind: 'provider' as const,
      label: t('admin.providerImport.sourceProvider'),
      provider: binding.providerName,
      tooltip: details.join('\n')
    }
  }
  if (props.account.type === 'apikey' || props.account.type === 'upstream') {
    return {
      kind: 'api' as const,
      label: 'API Key',
      provider: '',
      tooltip: t('admin.accounts.source.apiTooltip')
    }
  }
  return null
})
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

.account-source.is-api {
  background: #faf6e8;
  color: #5b5238;
}

.account-source.is-provider {
  background: #e0f2fe;
  color: #075985;
}

.account-source-label {
  flex-shrink: 0;
}

/* The provider name rides inside the chip after a hairline, truncated if long. */
.account-source-provider {
  min-width: 0;
  overflow: hidden;
  padding-left: 0.375rem;
  text-overflow: ellipsis;
  font-weight: 700;
  box-shadow: inset 1px 0 0 rgb(7 89 133 / 0.25);
}
</style>
