<template>
  <div class="usage-records" :class="flat ? 'usage-records--flat' : 'card overflow-hidden'">
    <div
      v-if="showIpGeoToolbar"
      class="flex items-center justify-end gap-2 px-4 py-2"
    >
      <span v-if="pendingIpCount > 0" class="text-xs text-gray-500 dark:text-gray-400">
        {{ t('usage.ipGeo.pending', { count: pendingIpCount }) }}
      </span>
      <button
        type="button"
        class="inline-flex items-center gap-1 rounded px-2 py-1 text-xs font-medium text-primary-600 transition-colors hover:bg-primary-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-primary-400 dark:hover:bg-primary-900/30"
        :disabled="ipGeoBatchLoading || pendingIpCount === 0"
        @click="handleBatchFetchIpGeo"
      >
        {{ ipGeoBatchLoading ? t('usage.ipGeo.batchFetching') : t('usage.ipGeo.batchFetch') }}
      </button>
    </div>
    <div class="usage-table-frame">
      <DataTable
        :columns="columns"
        :data="data"
        :loading="loading"
        :server-side-sort="serverSideSort"
        :default-sort-key="defaultSortKey"
        :default-sort-order="defaultSortOrder"
        @sort="(key, order) => $emit('sort', key, order)"
      >
        <template #cell-user="{ row }">
          <div class="text-sm">
            <button
              v-if="row.user?.email"
              class="font-medium text-primary-600 underline decoration-dashed underline-offset-2 transition-colors hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
              @click="$emit('userClick', row.user_id, row.user?.email)"
              :title="t('admin.usage.clickToViewBalance')"
            >
              {{ row.user.email }}
            </button>
            <span v-else class="font-medium text-gray-900 dark:text-white">-</span>
            <span v-if="row.user?.deleted_at" class="ml-1 inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-rose-100 text-rose-600 ring-1 ring-inset ring-rose-200 dark:bg-rose-500/20 dark:text-rose-400 dark:ring-rose-500/30">
              {{ t('admin.usage.userDeletedBadge') }}
            </span>
            <span class="ml-1 text-gray-500 dark:text-gray-400">#{{ row.user_id }}</span>
          </div>
        </template>

        <template #cell-api_key="{ row }">
          <span v-if="row.api_key?.name" class="usage-key" :title="row.api_key.name">
            <span class="usage-key-icon" aria-hidden="true"><Icon name="key" size="xs" :stroke-width="2" /></span>
            <span class="truncate">{{ row.api_key.name }}</span>
          </span>
          <span v-else class="usage-empty">-</span>
        </template>

        <template #cell-account="{ row }">
          <span class="text-sm text-gray-900 dark:text-white">{{ row.account?.name || '-' }}</span>
        </template>

        <template #cell-model="{ row }">
          <div class="flex min-w-0 max-w-[15rem] items-start gap-2">
            <span v-if="row.model" class="usage-model-mark" aria-hidden="true">
              <ModelIcon :model="row.model" size="15px" />
            </span>
            <div class="min-w-0 space-y-0.5 text-xs">
              <div v-if="row.model_mapping_chain && row.model_mapping_chain.includes('→')" class="space-y-0.5">
                <div v-for="(step, i) in row.model_mapping_chain.split('→')" :key="i"
                     class="break-all"
                     :class="i === 0 ? 'usage-model-name' : 'text-gray-500 dark:text-gray-400'"
                     :style="i > 0 ? `padding-left: ${i * 0.75}rem` : ''">
                  <span v-if="i > 0" class="mr-0.5">↳</span>{{ step }}
                </div>
              </div>
              <div v-else-if="row.upstream_model && row.upstream_model !== row.model" class="space-y-0.5">
                <div class="usage-model-name break-all">
                  {{ row.model }}
                </div>
                <div class="break-all text-gray-500 dark:text-gray-400">
                  <span class="mr-0.5">↳</span>{{ row.upstream_model }}
                </div>
              </div>
              <div v-else class="usage-model-name break-all">{{ row.model }}</div>
              <div
                v-if="row.upstream_model_mismatch === true && row.upstream_response_model"
                class="break-all pl-3 text-[11px]"
                :class="isLikelyModelVariant(row) ? 'text-amber-600 dark:text-amber-400' : 'text-orange-600 dark:text-orange-400'"
                :title="modelAuditTitle(row)"
              >
                <span class="mr-1">↳ {{ t('usage.upstreamResponseModel') }}:</span>{{ row.upstream_response_model }}
                <span
                  class="ml-1 inline-flex rounded px-1 py-px text-[10px] font-medium ring-1 ring-inset"
                  :class="isLikelyModelVariant(row)
                    ? 'bg-amber-50 text-amber-700 ring-amber-200 dark:bg-amber-500/10 dark:text-amber-300 dark:ring-amber-500/30'
                    : 'bg-orange-50 text-orange-700 ring-orange-200 dark:bg-orange-500/10 dark:text-orange-300 dark:ring-orange-500/30'"
                >
                  {{ isLikelyModelVariant(row) ? t('usage.modelVariant') : t('usage.modelMismatch') }}
                </span>
              </div>
            </div>
          </div>
        </template>

        <template #cell-reasoning_effort="{ row }">
          <span v-if="hasReasoningEffort(row)" class="usage-chip ring-1 ring-inset" :class="effortChipClass(row.reasoning_effort)">
            <span v-if="effortLevel(row.reasoning_effort) > 0" class="usage-effort-meter" aria-hidden="true">
              <i v-for="n in 4" :key="n" :class="{ 'is-on': n <= effortLevel(row.reasoning_effort) }"></i>
            </span>
            {{ formatReasoningEffort(row.reasoning_effort) }}
          </span>
          <span v-else class="usage-empty">-</span>
        </template>

        <template #cell-endpoint="{ row }">
          <div class="max-w-[16rem] space-y-1">
            <div class="usage-endpoint" :title="row.inbound_endpoint?.trim() || undefined">
              <span v-if="showUpstreamEndpoint" class="usage-endpoint-label">{{ t('usage.inbound') }}</span>
              <span class="truncate">{{ row.inbound_endpoint?.trim() || '-' }}</span>
            </div>
            <div v-if="showUpstreamEndpoint" class="usage-endpoint" :title="row.upstream_endpoint?.trim() || undefined">
              <span class="usage-endpoint-label">{{ t('usage.upstream') }}</span>
              <span class="truncate">{{ row.upstream_endpoint?.trim() || '-' }}</span>
            </div>
          </div>
        </template>

        <template #cell-group="{ row }">
          <span v-if="row.group" class="usage-chip usage-group-chip" :title="row.group.name">
            <span class="usage-dot" :style="{ background: platformAccentColor(row.group.platform || '') }" aria-hidden="true"></span>
            <span class="truncate">{{ row.group.name }}</span>
          </span>
          <span v-else class="usage-empty">-</span>
        </template>

        <template #cell-stream="{ row }">
          <span class="usage-chip ring-1 ring-inset" :class="getRequestTypeBadgeClass(row)">
            <Icon :name="getRequestTypeIcon(row)" size="xs" :stroke-width="2" />
            {{ getRequestTypeLabel(row) }}
          </span>
        </template>

        <template #cell-billing_mode="{ row }">
          <span class="usage-chip" :class="getBillingModeBadgeClass(getDisplayBillingMode(row))">
            {{ getBillingModeLabel(getDisplayBillingMode(row), t) }}
          </span>
        </template>

        <template #cell-tokens="{ row }">
          <!-- 图片生成请求（仅按次计费时显示图片格式） -->
          <div v-if="isImageUsage(row)" class="flex items-center gap-1.5 text-sm">
            <span class="usage-image-mark" aria-hidden="true">
              <svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" />
              </svg>
            </span>
            <span class="font-semibold tabular-nums text-gray-900 dark:text-white">{{ row.image_count }}{{ t('usage.imageUnit') }}</span>
            <span class="text-xs text-gray-400">({{ formatImageBillingSize(row, t) }})</span>
          </div>
          <span v-else-if="tokenTotal(row) === 0" class="usage-empty">-</span>
          <!-- Token 请求 -->
          <div v-else class="flex items-start gap-1.5">
            <div class="min-w-[8.5rem] space-y-1 text-sm">
              <div class="flex items-center gap-2.5">
                <div class="inline-flex items-center gap-1">
                  <Icon name="arrowDown" size="sm" class="h-3.5 w-3.5 text-emerald-500" />
                  <span class="font-semibold tabular-nums text-gray-900 dark:text-white">{{ row.input_tokens?.toLocaleString() || 0 }}</span>
                </div>
                <div class="inline-flex items-center gap-1">
                  <Icon name="arrowUp" size="sm" class="h-3.5 w-3.5 text-violet-500" />
                  <span class="font-semibold tabular-nums text-gray-900 dark:text-white">{{ row.output_tokens?.toLocaleString() || 0 }}</span>
                </div>
              </div>
              <div v-if="row.cache_read_tokens > 0 || row.cache_creation_tokens > 0" class="flex items-center gap-2.5 text-xs">
                <div v-if="row.cache_read_tokens > 0" class="inline-flex items-center gap-1">
                  <svg class="h-3.5 w-3.5 text-sky-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4" /></svg>
                  <span class="font-medium tabular-nums text-sky-600 dark:text-sky-400">{{ formatCacheTokens(row.cache_read_tokens) }}</span>
                </div>
                <div v-if="row.cache_creation_tokens > 0" class="inline-flex items-center gap-1">
                  <svg class="h-3.5 w-3.5 text-amber-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" /></svg>
                  <span class="font-medium tabular-nums text-amber-600 dark:text-amber-400">{{ formatCacheTokens(row.cache_creation_tokens) }}</span>
                  <span v-if="row.cache_creation_1h_tokens > 0" class="inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-orange-100 text-orange-600 ring-1 ring-inset ring-orange-200 dark:bg-orange-500/20 dark:text-orange-400 dark:ring-orange-500/30">1h</span>
                  <span v-if="row.cache_ttl_overridden" :title="t('usage.cacheTtlOverriddenHint')" class="inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-rose-100 text-rose-600 ring-1 ring-inset ring-rose-200 dark:bg-rose-500/20 dark:text-rose-400 dark:ring-rose-500/30 cursor-help">R</span>
                </div>
              </div>
              <div v-if="hasImageInputTokens(row) || hasImageOutputTokens(row)" class="flex items-center gap-2.5 text-xs">
                <div v-if="hasImageInputTokens(row)" class="inline-flex items-center gap-1">
                  <svg class="h-3.5 w-3.5 text-fuchsia-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" /></svg>
                  <span class="font-medium tabular-nums text-fuchsia-600 dark:text-fuchsia-400">{{ row.image_input_tokens.toLocaleString() }}</span>
                </div>
                <div v-if="hasImageOutputTokens(row)" class="inline-flex items-center gap-1">
                  <svg class="h-3.5 w-3.5 text-pink-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" /></svg>
                  <span class="font-medium tabular-nums text-pink-600 dark:text-pink-400">{{ row.image_output_tokens.toLocaleString() }}</span>
                </div>
              </div>
              <div class="usage-token-mix" role="img" :aria-label="tokenMixLabel(row)" :title="tokenMixLabel(row)">
                <span v-for="part in tokenMix(row)" :key="part.key" :class="part.cls" :style="{ flexGrow: part.value }"></span>
              </div>
            </div>
            <!-- Token Detail Tooltip -->
            <button
              type="button"
              class="usage-info-trigger group relative"
              :aria-label="t('usage.tokenDetails')"
              @mouseenter="showTokenTooltip($event, row)"
              @mouseleave="hideTokenTooltip"
              @focus="showTokenTooltip($event, row)"
              @blur="closeTooltips"
              @click.stop="pinTooltip('token', $event, row)"
            >
              <Icon name="infoCircle" size="xs" />
            </button>
          </div>
        </template>

        <template #cell-cache_hit_rate="{ row }">
          <span
            data-testid="cache-hit-rate"
            class="flex w-full min-w-[3.5rem] flex-col items-center justify-center gap-1 whitespace-nowrap text-sm font-semibold tabular-nums"
            :class="getCacheHitRateClass(row)"
          >
            <template v-if="getCacheHitRate(row) !== null">
              {{ formatCacheHitRate(row) }}
              <span class="usage-hit-bar" aria-hidden="true"><span :style="{ width: `${getCacheHitRate(row)}%` }"></span></span>
            </template>
            <template v-else>
              <span class="cache-hit-empty-mark" aria-hidden="true"></span>
              <span class="sr-only">-</span>
            </template>
          </span>
        </template>

        <template #cell-cost="{ row }">
          <div class="text-sm">
            <div class="flex items-center gap-1.5">
              <span class="usage-cost">${{ row.actual_cost?.toFixed(6) || '0.000000' }}</span>
              <span
                v-if="row.long_context_billing_applied"
                data-testid="long-context-billing-marker"
                :title="t('admin.usage.longContextPricingTooltip')"
                class="inline-flex items-center rounded px-1 py-px text-[10px] font-semibold leading-tight bg-amber-100 text-amber-700 ring-1 ring-inset ring-amber-200 dark:bg-amber-500/20 dark:text-amber-300 dark:ring-amber-500/30"
              >{{ t('admin.usage.longContext') }}</span>
              <!-- Cost Detail Tooltip -->
              <button
                type="button"
                class="usage-info-trigger group relative"
                :aria-label="t('usage.costDetails')"
                @mouseenter="showTooltip($event, row)"
                @mouseleave="hideTooltip"
                @focus="showTooltip($event, row)"
                @blur="closeTooltips"
                @click.stop="pinTooltip('cost', $event, row)"
              >
                <Icon name="infoCircle" size="xs" />
              </button>
            </div>
            <div v-if="hasRateAdjustment(row) || showsDiscountedOriginal(row)" class="mt-1 flex items-center gap-1.5 text-[11px]">
              <span v-if="hasRateAdjustment(row)" class="usage-rate-chip" :class="rateChipClass(row)" :title="t('usage.rate')">×{{ formatMultiplier(row.rate_multiplier) }}</span>
              <s v-if="showsDiscountedOriginal(row)" class="tabular-nums text-gray-400" :title="t('usage.original')">${{ row.total_cost.toFixed(6) }}</s>
            </div>
            <div v-if="showAccountBilling && row.account_rate_multiplier != null" class="mt-0.5 text-[11px] text-orange-500 dark:text-orange-400">
              A ${{ accountBilled(row).toFixed(6) }}
            </div>
            <ol v-if="row.billing_events?.length" class="mt-1.5 grid w-44 max-w-full gap-1 text-[11px]" data-testid="media-payment-events">
              <li
                v-for="event in row.billing_events"
                :key="event.stage"
                class="flex min-w-0 flex-wrap items-center justify-between gap-x-2 gap-y-0.5 whitespace-normal rounded-md px-2 py-1 text-left font-medium leading-4 ring-1 ring-inset"
                :class="mediaPaymentBadgeClass(event.stage)"
                :title="new Date(event.created_at).toLocaleString()"
              >
                <span class="min-w-0 break-words">{{ t(`usage.mediaPayment.${event.stage}`) }}</span>
                <span v-if="event.stage !== 'request_created'" class="ml-auto min-w-0 break-all text-right tabular-nums">{{ event.stage === 'refunded' ? '+' : event.stage === 'prepaid' ? '-' : '' }}${{ event.amount.toFixed(6) }}</span>
              </li>
            </ol>
          </div>
        </template>

        <template #cell-latency="{ row }">
          <div class="flex items-stretch gap-2.5 text-left" data-testid="usage-performance">
            <span
              class="w-1 shrink-0 rounded-full"
              :class="getLatencyBarClass(row)"
              aria-hidden="true"
            ></span>
            <div class="grid min-w-[8.5rem] grid-cols-[max-content_minmax(3.5rem,1fr)] items-baseline gap-x-3 gap-y-0.5 text-xs">
              <span class="text-gray-400 dark:text-gray-500">{{ t('usage.latencyFirstToken') }}</span>
              <span v-if="hasRecordedDuration(row.first_token_ms)" class="text-right font-semibold tabular-nums" :class="LATENCY_TEXT_CLASSES[firstTokenSeverity(row.first_token_ms)]">{{ formatDuration(row.first_token_ms) }}</span>
              <span v-else class="text-right text-[11px] text-gray-400 dark:text-gray-500">{{ t('usage.performanceNotRecorded') }}</span>
              <span class="text-gray-400 dark:text-gray-500">{{ t('usage.latencyDuration') }}</span>
              <span v-if="hasRecordedDuration(row.duration_ms)" class="text-right font-semibold tabular-nums" :class="LATENCY_TEXT_CLASSES[durationSeverity(row.duration_ms)]">{{ formatDuration(row.duration_ms) }}</span>
              <span v-else class="text-right text-[11px] text-gray-400 dark:text-gray-500">{{ t('usage.performanceNotRecorded') }}</span>
              <span class="text-gray-400 dark:text-gray-500" :title="t('usage.outputDeliverySpeedHint')">
                <span class="hidden sm:inline">{{ t('usage.outputDeliverySpeed') }}</span>
                <span class="sm:hidden">{{ t('usage.outputDeliverySpeedShort') }}</span>
              </span>
              <span
                v-if="getEndToEndOutputSpeed(row) !== null"
                data-testid="output-delivery-speed"
                class="whitespace-nowrap text-right font-semibold tabular-nums text-sky-600 dark:text-sky-400"
                :title="t('usage.outputDeliverySpeedHint')"
              >
                {{ getEndToEndOutputSpeed(row)?.toFixed(1) }}
                <span class="ml-0.5 text-[10px] font-normal text-gray-400 dark:text-gray-500">tok/s</span>
              </span>
              <span v-else data-testid="output-delivery-speed" class="text-right text-[11px] text-gray-400 dark:text-gray-500">{{ t(getOutputSpeedUnavailableReason(row)) }}</span>
            </div>
          </div>
        </template>

        <template #cell-created_at="{ value }">
          <div class="usage-time" :title="formatDateTime(value)">
            <span class="usage-time-clock">{{ formatClock(value) }}</span>
            <span class="usage-time-day">{{ formatDay(value) }}</span>
          </div>
        </template>

        <template #cell-request_id="{ row }">
          <div v-if="row.request_id" class="flex max-w-[160px] items-center gap-1.5">
            <span class="truncate font-mono text-xs text-gray-500 dark:text-gray-400" :title="row.request_id">
              {{ row.request_id }}
            </span>
            <button
              type="button"
              class="shrink-0 rounded p-0.5 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-dark-700 dark:hover:text-gray-300"
              :class="copiedRequestId === row.request_id ? 'text-green-500 hover:text-green-500' : ''"
              :title="copiedRequestId === row.request_id ? t('keys.copied') : t('keys.copyToClipboard')"
              @click="copyRequestId(row.request_id)"
            >
              <Icon :name="copiedRequestId === row.request_id ? 'check' : 'copy'" size="sm" class="h-3.5 w-3.5" />
            </button>
          </div>
          <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
        </template>

        <template #cell-user_agent="{ row }">
          <span v-if="row.user_agent" class="block max-w-[320px] truncate text-xs text-gray-600 dark:text-gray-400" :title="row.user_agent">{{ formatUserAgent(row.user_agent) }}</span>
          <span v-else class="usage-empty">-</span>
        </template>

        <template #cell-ip_address="{ row }">
          <div v-if="row.ip_address" class="max-w-[12rem]">
            <span class="block truncate font-mono text-xs text-gray-600 dark:text-gray-400" :title="row.ip_address">{{ row.ip_address }}</span>
            <IpGeoCell :ip="row.ip_address" />
          </div>
          <span v-else class="usage-empty">-</span>
        </template>

        <!-- Phones get one scannable card per request instead of a long label/value list. -->
        <template #mobile-card="{ row }">
          <article class="usage-card" :data-usage-card="row.request_id || row.id">
            <header class="usage-card-head">
              <span v-if="row.model" class="usage-model-mark usage-model-mark--card" aria-hidden="true">
                <ModelIcon :model="row.model" size="17px" />
              </span>
              <div class="usage-card-heading">
                <p class="usage-card-title" :title="row.model">{{ row.model || '-' }}</p>
                <p class="usage-card-meta" :title="formatDateTime(row.created_at)">
                  <span class="font-semibold text-gray-600">{{ formatClock(row.created_at) }}</span>
                  <span>{{ formatDay(row.created_at) }}</span>
                </p>
              </div>
              <div v-if="hasColumn('cost')" class="usage-card-price">
                <div class="flex items-center justify-end gap-1">
                  <span class="usage-card-cost">${{ row.actual_cost?.toFixed(6) || '0.000000' }}</span>
                  <button
                    type="button"
                    class="usage-info-trigger group relative"
                    :aria-label="t('usage.costDetails')"
                    @mouseenter="showTooltip($event, row)"
                    @mouseleave="hideTooltip"
                    @blur="closeTooltips"
                    @click.stop="pinTooltip('cost', $event, row)"
                  >
                    <Icon name="infoCircle" size="xs" />
                  </button>
                </div>
                <div v-if="hasRateAdjustment(row) || showsDiscountedOriginal(row) || row.long_context_billing_applied" class="mt-1 flex items-center justify-end gap-1 text-[11px]">
                  <span v-if="row.long_context_billing_applied" class="usage-rate-chip bg-amber-100 text-amber-700" :title="t('admin.usage.longContextPricingTooltip')">{{ t('admin.usage.longContext') }}</span>
                  <span v-if="hasRateAdjustment(row)" class="usage-rate-chip" :class="rateChipClass(row)">×{{ formatMultiplier(row.rate_multiplier) }}</span>
                  <s v-if="showsDiscountedOriginal(row)" class="tabular-nums text-gray-400">${{ row.total_cost.toFixed(6) }}</s>
                </div>
              </div>
            </header>

            <div class="mt-3 flex flex-wrap gap-1.5">
              <button
                v-if="hasColumn('user') && row.user?.email"
                type="button"
                class="usage-chip max-w-full bg-primary-100 text-primary-700"
                @click="$emit('userClick', row.user_id, row.user?.email)"
              >
                <span class="truncate">{{ row.user.email }}</span>
              </button>
              <span v-if="hasColumn('api_key') && row.api_key?.name" class="usage-chip usage-key-chip" :title="row.api_key.name">
                <Icon name="key" size="xs" :stroke-width="2" />
                <span class="truncate">{{ row.api_key.name }}</span>
              </span>
              <span v-if="hasColumn('account') && row.account?.name" class="usage-chip bg-gray-100 text-gray-600" :title="row.account.name">
                <span class="truncate">{{ row.account.name }}</span>
              </span>
              <span v-if="hasColumn('group') && row.group" class="usage-chip usage-group-chip" :title="row.group.name">
                <span class="usage-dot" :style="{ background: platformAccentColor(row.group.platform || '') }" aria-hidden="true"></span>
                <span class="truncate">{{ row.group.name }}</span>
              </span>
              <span v-if="hasColumn('stream')" class="usage-chip ring-1 ring-inset" :class="getRequestTypeBadgeClass(row)">
                <Icon :name="getRequestTypeIcon(row)" size="xs" :stroke-width="2" />
                {{ getRequestTypeLabel(row) }}
              </span>
              <span v-if="hasColumn('billing_mode')" class="usage-chip" :class="getBillingModeBadgeClass(getDisplayBillingMode(row))">
                {{ getBillingModeLabel(getDisplayBillingMode(row), t) }}
              </span>
              <span v-if="hasColumn('reasoning_effort') && hasReasoningEffort(row)" class="usage-chip ring-1 ring-inset" :class="effortChipClass(row.reasoning_effort)">
                <span v-if="effortLevel(row.reasoning_effort) > 0" class="usage-effort-meter" aria-hidden="true">
                  <i v-for="n in 4" :key="n" :class="{ 'is-on': n <= effortLevel(row.reasoning_effort) }"></i>
                </span>
                {{ formatReasoningEffort(row.reasoning_effort) }}
              </span>
            </div>

            <div v-if="hasColumn('tokens') && isImageUsage(row)" class="usage-card-panel mt-3 flex items-center gap-2 text-sm">
              <span class="usage-image-mark" aria-hidden="true">
                <svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" />
                </svg>
              </span>
              <span class="font-semibold tabular-nums text-gray-900">{{ row.image_count }}{{ t('usage.imageUnit') }}</span>
              <span class="text-xs text-gray-400">{{ formatImageBillingSize(row, t) }}</span>
            </div>
            <div v-else-if="hasColumn('tokens') && tokenTotal(row) > 0" class="usage-card-panel mt-3">
              <div class="grid grid-cols-3 gap-2">
                <div class="usage-stat">
                  <span class="usage-stat-label"><i class="usage-dot bg-emerald-400"></i>{{ t('usage.in') }}</span>
                  <span class="usage-stat-value">{{ formatCacheTokens(row.input_tokens || 0) }}</span>
                </div>
                <div class="usage-stat">
                  <span class="usage-stat-label"><i class="usage-dot bg-violet-400"></i>{{ t('usage.out') }}</span>
                  <span class="usage-stat-value">{{ formatCacheTokens(row.output_tokens || 0) }}</span>
                </div>
                <div v-if="hasColumn('cache_hit_rate')" class="usage-stat">
                  <span class="usage-stat-label"><i class="usage-dot bg-sky-400"></i>{{ t('usage.cacheHitRate') }}</span>
                  <span class="usage-stat-value" :class="getCacheHitRateClass(row)">{{ formatCacheHitRate(row) }}</span>
                </div>
                <div v-else class="usage-stat">
                  <span class="usage-stat-label"><i class="usage-dot bg-sky-400"></i>{{ t('usage.cacheRead') }}</span>
                  <span class="usage-stat-value">{{ formatCacheTokens(row.cache_read_tokens || 0) }}</span>
                </div>
              </div>
              <div class="mt-2.5 flex items-center gap-2">
                <div class="usage-token-mix flex-1" role="img" :aria-label="tokenMixLabel(row)">
                  <span v-for="part in tokenMix(row)" :key="part.key" :class="part.cls" :style="{ flexGrow: part.value }"></span>
                </div>
                <button
                  type="button"
                  class="usage-info-trigger group relative"
                  :aria-label="t('usage.tokenDetails')"
                  @mouseenter="showTokenTooltip($event, row)"
                  @mouseleave="hideTokenTooltip"
                  @blur="closeTooltips"
                  @click.stop="pinTooltip('token', $event, row)"
                >
                  <Icon name="infoCircle" size="xs" />
                </button>
              </div>
              <p v-if="row.cache_read_tokens > 0 || row.cache_creation_tokens > 0" class="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-gray-500">
                <span v-if="row.cache_read_tokens > 0">{{ t('usage.cacheHit') }} <b class="font-semibold tabular-nums text-sky-600">{{ formatCacheTokens(row.cache_read_tokens) }}</b></span>
                <span v-if="row.cache_creation_tokens > 0">
                  {{ t('usage.cacheCreate') }} <b class="font-semibold tabular-nums text-amber-600">{{ formatCacheTokens(row.cache_creation_tokens) }}</b>
                  <span v-if="row.cache_creation_1h_tokens > 0" class="ml-0.5 rounded bg-orange-100 px-1 text-[10px] font-medium text-orange-600">1h</span>
                </span>
              </p>
            </div>

            <div v-if="hasColumn('latency')" class="usage-card-perf mt-3">
              <span class="usage-perf-dot" :class="getLatencyBarClass(row)" aria-hidden="true"></span>
              <span v-if="hasRecordedDuration(row.first_token_ms)">{{ t('usage.latencyFirstToken') }}
                <b :class="LATENCY_TEXT_CLASSES[firstTokenSeverity(row.first_token_ms)]">{{ formatDuration(row.first_token_ms) }}</b>
              </span>
              <span>{{ t('usage.latencyDuration') }}
                <b v-if="hasRecordedDuration(row.duration_ms)" :class="LATENCY_TEXT_CLASSES[durationSeverity(row.duration_ms)]">{{ formatDuration(row.duration_ms) }}</b>
                <span v-else class="text-gray-400">{{ t('usage.performanceNotRecorded') }}</span>
              </span>
              <span v-if="getEndToEndOutputSpeed(row) !== null" :title="t('usage.outputDeliverySpeedHint')">
                <b class="text-sky-600">{{ getEndToEndOutputSpeed(row)?.toFixed(1) }}</b> tok/s
              </span>
            </div>

            <ol v-if="hasColumn('cost') && row.billing_events?.length" class="mt-3 grid gap-1 text-[11px]">
              <li
                v-for="event in row.billing_events"
                :key="event.stage"
                class="flex items-center justify-between gap-2 rounded-lg px-2.5 py-1.5 font-medium ring-1 ring-inset"
                :class="mediaPaymentBadgeClass(event.stage)"
              >
                <span>{{ t(`usage.mediaPayment.${event.stage}`) }}</span>
                <span v-if="event.stage !== 'request_created'" class="tabular-nums">{{ event.stage === 'refunded' ? '+' : event.stage === 'prepaid' ? '-' : '' }}${{ event.amount.toFixed(6) }}</span>
              </li>
            </ol>

            <footer v-if="mobileFooterVisible(row)" class="usage-card-footer mt-3">
              <span v-if="hasColumn('endpoint') && row.inbound_endpoint?.trim()" class="usage-card-footer-item font-mono" :title="row.inbound_endpoint">
                <Icon name="link" size="xs" class="shrink-0" />
                <span class="truncate">{{ row.inbound_endpoint.trim() }}</span>
              </span>
              <span v-if="hasColumn('ip_address') && row.ip_address" class="usage-card-footer-item">
                <Icon name="globe" size="xs" class="shrink-0" />
                <span class="truncate font-mono">{{ row.ip_address }}</span>
                <IpGeoCell :ip="row.ip_address" class="shrink-0" />
              </span>
              <span v-if="hasColumn('request_id') && row.request_id" class="usage-card-footer-item font-mono" :title="row.request_id">
                <span class="truncate">{{ row.request_id }}</span>
              </span>
              <span v-if="hasColumn('user_agent') && row.user_agent" class="usage-card-footer-item" :title="row.user_agent">
                <span class="truncate">{{ formatUserAgent(row.user_agent) }}</span>
              </span>
            </footer>
          </article>
        </template>

        <template #empty><EmptyState :message="t('usage.noRecords')" /></template>
      </DataTable>
    </div>
  </div>

  <!-- Token Tooltip Portal -->
  <Teleport to="body">
    <div
      v-if="tokenTooltipVisible"
      ref="tokenTooltipRef"
      class="fixed z-[9999] pointer-events-none"
      :class="{ '-translate-y-1/2': isSideTooltip(tokenTooltipPosition.placement) }"
      :style="{
        left: tokenTooltipPosition.x + 'px',
        top: tokenTooltipPosition.y + 'px'
      }"
    >
      <div class="usage-tooltip-panel whitespace-nowrap rounded-lg border border-gray-700 bg-gray-900 px-3 py-2.5 text-xs text-white shadow-xl dark:border-gray-600 dark:bg-gray-800">
        <div class="space-y-1.5">
          <div>
            <div class="text-xs font-semibold text-gray-300 mb-1">{{ t('usage.tokenDetails') }}</div>
            <div v-if="tokenTooltipData && tokenTooltipData.input_tokens > 0 && !hasImageInputTokens(tokenTooltipData)" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.inputTokens') }}</span>
              <span class="font-medium text-white">{{ tokenTooltipData.input_tokens.toLocaleString() }}</span>
            </div>
            <div v-if="tokenTooltipData && hasImageInputTokens(tokenTooltipData) && textInputTokens(tokenTooltipData) > 0" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.inputTokens') }}</span>
              <span class="font-medium text-white">{{ textInputTokens(tokenTooltipData).toLocaleString() }}</span>
            </div>
            <div v-if="tokenTooltipData && hasImageInputTokens(tokenTooltipData)" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('usage.imageInputTokens') }}</span>
              <span class="font-medium text-fuchsia-300">{{ tokenTooltipData.image_input_tokens.toLocaleString() }}</span>
            </div>
            <div v-if="tokenTooltipData && tokenTooltipData.output_tokens > 0 && !hasImageOutputTokens(tokenTooltipData)" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.outputTokens') }}</span>
              <span class="font-medium text-white">{{ tokenTooltipData.output_tokens.toLocaleString() }}</span>
            </div>
            <div v-if="tokenTooltipData && hasImageOutputTokens(tokenTooltipData) && textOutputTokens(tokenTooltipData) > 0" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.outputTokens') }}</span>
              <span class="font-medium text-white">{{ textOutputTokens(tokenTooltipData).toLocaleString() }}</span>
            </div>
            <div v-if="tokenTooltipData && hasImageOutputTokens(tokenTooltipData)" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('usage.imageOutputTokens') }}</span>
              <span class="font-medium text-pink-300">{{ tokenTooltipData.image_output_tokens.toLocaleString() }}</span>
            </div>
            <div v-if="tokenTooltipData && tokenTooltipData.cache_creation_tokens > 0">
              <!-- 有 5m/1h 明细时，展开显示 -->
              <template v-if="tokenTooltipData.cache_creation_5m_tokens > 0 || tokenTooltipData.cache_creation_1h_tokens > 0">
                <div v-if="tokenTooltipData.cache_creation_5m_tokens > 0" class="flex items-center justify-between gap-4">
                  <span class="text-gray-400 flex items-center gap-1.5">
                    {{ t('admin.usage.cacheCreation5mTokens') }}
                    <span class="inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-amber-500/20 text-amber-400 ring-1 ring-inset ring-amber-500/30">5m</span>
                  </span>
                  <span class="font-medium text-white">{{ tokenTooltipData.cache_creation_5m_tokens.toLocaleString() }}</span>
                </div>
                <div v-if="tokenTooltipData.cache_creation_1h_tokens > 0" class="flex items-center justify-between gap-4">
                  <span class="text-gray-400 flex items-center gap-1.5">
                    {{ t('admin.usage.cacheCreation1hTokens') }}
                    <span class="inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-orange-500/20 text-orange-400 ring-1 ring-inset ring-orange-500/30">1h</span>
                  </span>
                  <span class="font-medium text-white">{{ tokenTooltipData.cache_creation_1h_tokens.toLocaleString() }}</span>
                </div>
              </template>
              <!-- 无明细时，只显示聚合值 -->
              <div v-else class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('admin.usage.cacheCreationTokens') }}</span>
                <span class="font-medium text-white">{{ tokenTooltipData.cache_creation_tokens.toLocaleString() }}</span>
              </div>
            </div>
            <div v-if="tokenTooltipData && tokenTooltipData.cache_ttl_overridden" class="flex items-center justify-between gap-4">
              <span class="text-gray-400 flex items-center gap-1.5">
                {{ t('usage.cacheTtlOverriddenLabel') }}
                <span class="inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-rose-500/20 text-rose-400 ring-1 ring-inset ring-rose-500/30">R-{{ tokenTooltipData.cache_creation_1h_tokens > 0 ? '5m' : '1H' }}</span>
              </span>
              <span class="font-medium text-rose-400">{{ tokenTooltipData.cache_creation_1h_tokens > 0 ? t('usage.cacheTtlOverridden1h') : t('usage.cacheTtlOverridden5m') }}</span>
            </div>
            <div v-if="tokenTooltipData && tokenTooltipData.cache_read_tokens > 0" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.cacheReadTokens') }}</span>
              <span class="font-medium text-white">{{ tokenTooltipData.cache_read_tokens.toLocaleString() }}</span>
            </div>
          </div>
          <div class="flex items-center justify-between gap-6 border-t border-gray-700 pt-1.5">
            <span class="text-gray-400">{{ t('usage.totalTokens') }}</span>
            <span class="font-semibold text-blue-400">{{ ((tokenTooltipData?.input_tokens || 0) + (tokenTooltipData?.output_tokens || 0) + (tokenTooltipData?.cache_creation_tokens || 0) + (tokenTooltipData?.cache_read_tokens || 0)).toLocaleString() }}</span>
          </div>
        </div>
        <div v-if="tokenTooltipPosition.placement === 'right'" class="absolute right-full top-1/2 h-0 w-0 -translate-y-1/2 border-b-[6px] border-r-[6px] border-t-[6px] border-b-transparent border-r-gray-900 border-t-transparent dark:border-r-gray-800"></div>
        <div v-else-if="tokenTooltipPosition.placement === 'left'" class="absolute left-full top-1/2 h-0 w-0 -translate-y-1/2 border-b-[6px] border-l-[6px] border-t-[6px] border-b-transparent border-l-gray-900 border-t-transparent dark:border-l-gray-800"></div>
      </div>
    </div>
  </Teleport>

  <!-- Cost Tooltip Portal -->
  <Teleport to="body">
    <div
      v-if="tooltipVisible"
      ref="costTooltipRef"
      class="fixed z-[9999] pointer-events-none"
      :class="{ '-translate-y-1/2': isSideTooltip(tooltipPosition.placement) }"
      :style="{
        left: tooltipPosition.x + 'px',
        top: tooltipPosition.y + 'px'
      }"
    >
      <div class="usage-tooltip-panel whitespace-nowrap rounded-lg border border-gray-700 bg-gray-900 px-3 py-2.5 text-xs text-white shadow-xl dark:border-gray-600 dark:bg-gray-800">
        <div class="space-y-1.5">
          <!-- Cost Breakdown -->
          <div class="mb-2 border-b border-gray-700 pb-1.5">
            <div class="text-xs font-semibold text-gray-300 mb-1">{{ t('usage.costDetails') }}</div>
            <div v-if="tooltipData && tooltipData.input_cost > 0" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.inputCost') }}</span>
              <span class="font-medium text-white">${{ tooltipData.input_cost.toFixed(6) }}</span>
            </div>
            <div v-if="tooltipData && hasImageInputCost(tooltipData)" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('usage.imageInputCost') }}</span>
              <span class="font-medium text-fuchsia-300">${{ tooltipData.image_input_cost.toFixed(6) }}</span>
            </div>
            <div v-if="tooltipData && tooltipData.output_cost > 0" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.outputCost') }}</span>
              <span class="font-medium text-white">${{ tooltipData.output_cost.toFixed(6) }}</span>
            </div>
            <div v-if="tooltipData && hasImageOutputCost(tooltipData)" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('usage.imageOutputCost') }}</span>
              <span class="font-medium text-pink-300">${{ tooltipData.image_output_cost.toFixed(6) }}</span>
            </div>
            <!-- Token billing: show unit prices per 1M tokens -->
            <template v-if="tooltipData && !isImageUsage(tooltipData) && (!tooltipData.billing_mode || tooltipData.billing_mode === BILLING_MODE_TOKEN)">
              <div v-if="tooltipData && textInputTokens(tooltipData) > 0" class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.inputTokenPrice') }}</span>
                <span class="font-medium text-sky-300">{{ formatTokenPricePerMillion(tooltipData.input_cost, textInputTokens(tooltipData)) }} {{ t('usage.perMillionTokens') }}</span>
              </div>
              <div v-if="tooltipData && hasImageInputTokens(tooltipData)" class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.imageInputTokenPrice') }}</span>
                <span class="font-medium text-fuchsia-300">{{ formatTokenPricePerMillion(tooltipData.image_input_cost ?? 0, tooltipData.image_input_tokens) }} {{ t('usage.perMillionTokens') }}</span>
              </div>
              <div v-if="tooltipData && tooltipData.output_cost > 0 && textOutputTokens(tooltipData) > 0" class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.outputTokenPrice') }}</span>
                <span class="font-medium text-violet-300">{{ formatTokenPricePerMillion(tooltipData.output_cost, textOutputTokens(tooltipData)) }} {{ t('usage.perMillionTokens') }}</span>
              </div>
              <div v-if="tooltipData && hasImageOutputTokens(tooltipData)" class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.imageOutputTokenPrice') }}</span>
                <span class="font-medium text-pink-300">{{ formatTokenPricePerMillion(tooltipData.image_output_cost ?? 0, tooltipData.image_output_tokens) }} {{ t('usage.perMillionTokens') }}</span>
              </div>
            </template>
            <template v-else-if="tooltipData && isImageUsage(tooltipData)">
              <div class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.imageCount') }}</span>
                <span class="font-medium text-white">{{ tooltipData.image_count }}{{ t('usage.imageUnit') }}</span>
              </div>
              <div class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.imageBillingSize') }}</span>
                <span class="font-medium text-white">{{ formatImageBillingSize(tooltipData, t) }}</span>
              </div>
              <div class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.imageSizeSource') }}</span>
                <span class="font-medium text-white">{{ formatImageSizeSource(tooltipData, t) }}</span>
              </div>
              <div class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.imageInputSize') }}</span>
                <span class="font-medium text-white">{{ formatImageInputSize(tooltipData, t) }}</span>
              </div>
              <div class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.imageOutputSize') }}</span>
                <span class="font-medium text-white">{{ formatImageOutputSize(tooltipData, t) }}</span>
              </div>
              <div v-if="formatImageSizeBreakdown(tooltipData)" class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.imageSizeBreakdown') }}</span>
                <span class="font-medium text-white">{{ formatImageSizeBreakdown(tooltipData) }}</span>
              </div>
              <div class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.imageUnitPrice') }}</span>
                <span class="font-medium text-sky-300">${{ imageUnitPrice(tooltipData).toFixed(6) }}</span>
              </div>
              <div class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.imageTotalPrice') }}</span>
                <span class="font-medium text-white">${{ tooltipData.total_cost?.toFixed(6) || '0.000000' }}</span>
              </div>
            </template>
            <div v-else class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('usage.unitPrice') }}</span>
              <span class="font-medium text-sky-300">${{ tooltipData?.total_cost?.toFixed(6) || '0.000000' }}</span>
            </div>
            <div v-if="tooltipData && tooltipData.cache_creation_cost > 0" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.cacheCreationCost') }}</span>
              <span class="font-medium text-white">${{ tooltipData.cache_creation_cost.toFixed(6) }}</span>
            </div>
            <div v-if="tooltipData && tooltipData.cache_read_cost > 0" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.cacheReadCost') }}</span>
              <span class="font-medium text-white">${{ tooltipData.cache_read_cost.toFixed(6) }}</span>
            </div>
          </div>
          <!-- Rate and Summary -->
          <div class="flex items-center justify-between gap-6">
            <span class="text-gray-400">{{ t('usage.serviceTier') }}</span>
            <span class="font-semibold text-cyan-300">{{ getUsageServiceTierLabel(tooltipData?.service_tier, t) }}</span>
          </div>
          <div class="flex items-center justify-between gap-6">
            <span class="text-gray-400">{{ t('usage.rate') }}</span>
            <span class="font-semibold text-blue-400">{{ formatMultiplier(tooltipData?.rate_multiplier ?? 1) }}x</span>
          </div>
          <div class="flex items-center justify-between gap-6">
            <span class="text-gray-400">{{ t('usage.original') }}</span>
            <span class="font-medium text-white">${{ tooltipData?.total_cost?.toFixed(6) || '0.000000' }}</span>
          </div>
          <div class="flex items-center justify-between gap-6">
            <span class="text-gray-400">{{ t('usage.userBilled') }}</span>
            <span class="font-semibold text-green-400">${{ tooltipData?.actual_cost?.toFixed(6) || '0.000000' }}</span>
          </div>
          <!-- Account billing (separated from user billing) -->
          <template v-if="showAccountBilling">
            <div class="flex items-center justify-between gap-6 border-t border-gray-700 pt-1.5">
              <span class="text-gray-400">{{ t('usage.accountMultiplier') }}</span>
              <span class="font-semibold text-blue-400">{{ formatMultiplier(tooltipData?.account_rate_multiplier ?? 1) }}x</span>
            </div>
            <div class="flex items-center justify-between gap-6">
              <span class="text-gray-400">{{ t('usage.accountBilled') }}</span>
              <span class="font-semibold text-green-400">
                ${{ accountBilled({
                  total_cost: tooltipData?.total_cost,
                  account_stats_cost: tooltipData?.account_stats_cost,
                  account_rate_multiplier: tooltipData?.account_rate_multiplier,
                }).toFixed(6) }}
              </span>
            </div>
          </template>
        </div>
        <div v-if="tooltipPosition.placement === 'right'" class="absolute right-full top-1/2 h-0 w-0 -translate-y-1/2 border-b-[6px] border-r-[6px] border-t-[6px] border-b-transparent border-r-gray-900 border-t-transparent dark:border-r-gray-800"></div>
        <div v-else-if="tooltipPosition.placement === 'left'" class="absolute left-full top-1/2 h-0 w-0 -translate-y-1/2 border-b-[6px] border-l-[6px] border-t-[6px] border-b-transparent border-l-gray-900 border-t-transparent dark:border-l-gray-800"></div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { formatDate, formatDateTime, formatReasoningEffort } from '@/utils/format'
import { formatCacheTokens, formatMultiplier } from '@/utils/formatters'
import { formatTokenPricePerMillion } from '@/utils/usagePricing'
import { formatCacheHitRate, getCacheHitRate, getCacheHitRateClass } from '@/utils/cacheHitRate'
import { getUsageServiceTierLabel } from '@/utils/usageServiceTier'
import { resolveUsageRequestType } from '@/utils/usageRequestType'
import { getEndToEndOutputSpeed, getOutputSpeedUnavailableReason } from '@/utils/outputSpeed'
import { platformAccentColor } from '@/utils/platformColors'
import {
  LATENCY_BAR_CLASSES,
  LATENCY_BAR_FROM_CLASSES,
  LATENCY_BAR_TO_CLASSES,
  LATENCY_TEXT_CLASSES,
  durationSeverity,
  firstTokenSeverity,
} from '@/utils/latencyHealth'
import {
  BILLING_MODE_TOKEN,
  getBillingModeLabel,
  getBillingModeBadgeClass,
  isImageUsage,
  getDisplayBillingMode,
  imageUnitPrice,
} from '@/utils/billingMode'
import {
  formatImageBillingSize,
  formatImageInputSize,
  formatImageOutputSize,
  formatImageSizeBreakdown,
  formatImageSizeSource,
  hasImageOutputTokens,
  textOutputTokens,
  hasImageOutputCost,
  hasImageInputTokens,
  textInputTokens,
  hasImageInputCost,
} from '@/utils/imageUsage'

/** Compute the account-billed cost for display: (account_stats_cost ?? total_cost) * rate_multiplier */
function accountBilled(row: { total_cost?: number | null; account_stats_cost?: number | null; account_rate_multiplier?: number | null }): number {
  const base = row.account_stats_cost != null ? row.account_stats_cost : (row.total_cost ?? 0)
  const result = base * (row.account_rate_multiplier ?? 1)
  return Number.isNaN(result) ? 0 : result
}


import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import IpGeoCell from '@/components/common/IpGeoCell.vue'
import ModelIcon from '@/components/common/ModelIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import { fetchBatch, getEntry } from '@/utils/ipGeoLookup'
import type { AdminUsageLog } from '@/types'
import type { Column } from '@/components/common/types'

interface Props {
  data: AdminUsageLog[]
  loading?: boolean
  columns: Column[]
  serverSideSort?: boolean
  defaultSortKey?: string
  defaultSortOrder?: 'asc' | 'desc'
  showAccountBilling?: boolean
  showUpstreamEndpoint?: boolean
  /** 嵌入统一卡片内使用：去掉自身卡片外观 */
  flat?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  loading: false,
  serverSideSort: false,
  defaultSortKey: '',
  defaultSortOrder: 'asc',
  showAccountBilling: true,
  showUpstreamEndpoint: true,
  flat: false
})
const emit = defineEmits<{
  userClick: [userID: number, email?: string]
  sort: [key: string, order: 'asc' | 'desc']
  ipGeoBatchFailed: []
}>()
const { t } = useI18n()
const appStore = useAppStore()
const copiedRequestId = ref<string | null>(null)
const showAccountBilling = props.showAccountBilling
const showUpstreamEndpoint = props.showUpstreamEndpoint
const ipGeoBatchLoading = ref(false)

const showIpGeoToolbar = computed(() => props.columns.some((col) => col.key === 'ip_address'))

const MEDIA_PAYMENT_BADGE_CLASSES = {
  request_created: 'bg-sky-500/10 text-sky-700 ring-sky-500/15',
  prepaid: 'bg-amber-500/10 text-amber-800 ring-amber-500/15',
  settled: 'bg-emerald-500/10 text-emerald-700 ring-emerald-500/15',
  refunded: 'bg-violet-500/10 text-violet-700 ring-violet-500/15',
} as const

const mediaPaymentBadgeClass = (stage: keyof typeof MEDIA_PAYMENT_BADGE_CLASSES): string => MEDIA_PAYMENT_BADGE_CLASSES[stage]

const sentUpstreamModel = (row: AdminUsageLog): string => row.upstream_model?.trim() || row.model?.trim() || ''

const normalizeModelVariant = (model: string): string => model
  .trim()
  .toLowerCase()
  .replace(/-latest$/, '')
  .replace(/-\d{4}-\d{2}-\d{2}$/, '')
  .replace(/-\d{8}$/, '')

const isLikelyModelVariant = (row: AdminUsageLog): boolean => {
  const sent = sentUpstreamModel(row)
  const response = row.upstream_response_model?.trim() || ''
  return sent !== '' && response !== '' && normalizeModelVariant(sent) === normalizeModelVariant(response)
}

const modelAuditTitle = (row: AdminUsageLog): string => [
  `${t('usage.requestedModel')}: ${row.model || '-'}`,
  `${t('usage.sentUpstreamModel')}: ${sentUpstreamModel(row) || '-'}`,
  `${t('usage.upstreamResponseModel')}: ${row.upstream_response_model || '-'}`,
].join('\n')

const currentPageIps = computed(() =>
  Array.from(new Set(props.data.map((row) => row.ip_address).filter((ip): ip is string => Boolean(ip))))
)

const pendingIpCount = computed(() => {
  if (!showIpGeoToolbar.value) return 0
  return currentPageIps.value.filter((ip) => {
    const status = getEntry(ip).status
    return status === 'idle' || status === 'error'
  }).length
})

const handleBatchFetchIpGeo = async () => {
  ipGeoBatchLoading.value = true
  try {
    const ok = await fetchBatch(currentPageIps.value)
    if (!ok) emit('ipGeoBatchFailed')
  } finally {
    ipGeoBatchLoading.value = false
  }
}

const copyRequestId = async (requestId: string) => {
  try {
    await navigator.clipboard.writeText(requestId)
    copiedRequestId.value = requestId
    appStore.showSuccess(t('admin.usage.requestIdCopied'))
    window.setTimeout(() => {
      if (copiedRequestId.value === requestId) copiedRequestId.value = null
    }, 2000)
  } catch {
    appStore.showError(t('common.copyFailed'))
  }
}

// Tooltip state - cost
type TooltipPlacement = 'right' | 'left' | 'below' | 'above'
interface TooltipPosition { x: number; y: number; placement: TooltipPlacement }

const tooltipVisible = ref(false)
const tooltipPosition = ref<TooltipPosition>({ x: 0, y: 0, placement: 'right' })
const tooltipData = ref<AdminUsageLog | null>(null)
const costTooltipRef = ref<HTMLElement | null>(null)

// Tooltip state - token
const tokenTooltipVisible = ref(false)
const tokenTooltipPosition = ref<TooltipPosition>({ x: 0, y: 0, placement: 'right' })
const tokenTooltipData = ref<AdminUsageLog | null>(null)
const tokenTooltipRef = ref<HTMLElement | null>(null)

const columnKeys = computed(() => new Set(props.columns.map((col) => col.key)))
const hasColumn = (key: string): boolean => columnKeys.value.has(key)

const mobileFooterVisible = (row: AdminUsageLog): boolean =>
  (hasColumn('endpoint') && Boolean(row.inbound_endpoint?.trim())) ||
  (hasColumn('ip_address') && Boolean(row.ip_address)) ||
  (hasColumn('request_id') && Boolean(row.request_id)) ||
  (hasColumn('user_agent') && Boolean(row.user_agent))

const formatClock = (value: string | null | undefined): string =>
  formatDate(value, { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })

const formatDay = (value: string | null | undefined): string =>
  formatDate(value, { year: 'numeric', month: '2-digit', day: '2-digit' })

const EFFORT_LEVELS: Record<string, number> = { low: 1, medium: 2, high: 3, xhigh: 4, extrahigh: 4, max: 4 }
const EFFORT_CHIP_CLASSES = [
  'bg-gray-100 text-gray-600 ring-gray-200',
  'bg-emerald-50 text-emerald-700 ring-emerald-200',
  'bg-sky-50 text-sky-700 ring-sky-200',
  'bg-amber-50 text-amber-700 ring-amber-200',
  'bg-rose-50 text-rose-700 ring-rose-200',
]

const effortLevel = (effort: string | null | undefined): number =>
  EFFORT_LEVELS[(effort ?? '').toString().trim().toLowerCase().replace(/[-_\s]/g, '')] ?? 0

const hasReasoningEffort = (row: AdminUsageLog): boolean => formatReasoningEffort(row.reasoning_effort) !== '-'

const effortChipClass = (effort: string | null | undefined): string => EFFORT_CHIP_CLASSES[effortLevel(effort)]

/** Total tokens of a request; video and per-call rows can legitimately carry none. */
const tokenTotal = (row: AdminUsageLog): number =>
  (row.input_tokens || 0) + (row.output_tokens || 0) + (row.cache_read_tokens || 0) + (row.cache_creation_tokens || 0)

const TOKEN_MIX_PARTS = [
  { key: 'input', field: 'input_tokens', cls: 'bg-emerald-400', label: 'admin.usage.inputTokens' },
  { key: 'output', field: 'output_tokens', cls: 'bg-violet-400', label: 'admin.usage.outputTokens' },
  { key: 'cache_read', field: 'cache_read_tokens', cls: 'bg-sky-400', label: 'admin.usage.cacheReadTokens' },
  { key: 'cache_creation', field: 'cache_creation_tokens', cls: 'bg-amber-400', label: 'admin.usage.cacheCreationTokens' },
] as const

/** Share of each token kind, rendered as a thin stacked bar so the cost driver stands out at a glance. */
const tokenMix = (row: AdminUsageLog) => TOKEN_MIX_PARTS
  .map((part) => ({ key: part.key, cls: part.cls, label: part.label, value: Math.max(0, row[part.field] || 0) }))
  .filter((part) => part.value > 0)

const tokenMixLabel = (row: AdminUsageLog): string => {
  const total = tokenTotal(row)
  if (total <= 0) return ''
  return tokenMix(row).map((part) => `${t(part.label)} ${Math.round((part.value / total) * 100)}%`).join(' · ')
}

const hasRateAdjustment = (row: AdminUsageLog): boolean =>
  typeof row.rate_multiplier === 'number' && Number.isFinite(row.rate_multiplier) && Math.abs(row.rate_multiplier - 1) > 1e-9

/** Struck-through list price, shown only when the user actually paid less. */
const showsDiscountedOriginal = (row: AdminUsageLog): boolean =>
  typeof row.total_cost === 'number' && typeof row.actual_cost === 'number' && row.total_cost - row.actual_cost >= 0.0000005

const rateChipClass = (row: AdminUsageLog): string =>
  (row.rate_multiplier ?? 1) < 1 ? 'bg-emerald-100 text-emerald-700' : 'bg-amber-100 text-amber-700'

const getRequestTypeLabel = (row: AdminUsageLog): string => {
  const requestType = resolveUsageRequestType(row)
  if (requestType === 'cyber') return t('usage.cyber')
  if (requestType === 'live') return t('usage.live')
  if (requestType === 'ws_v2') return t('usage.ws')
  if (requestType === 'stream') return t('usage.stream')
  if (requestType === 'sync') return t('usage.sync')
  return t('usage.unknown')
}

const getRequestTypeBadgeClass = (row: AdminUsageLog): string => {
  const requestType = resolveUsageRequestType(row)
  if (requestType === 'cyber') return 'bg-red-50 text-red-700 ring-red-200 dark:bg-red-900 dark:text-red-200'
  if (requestType === 'live') return 'bg-emerald-50 text-emerald-700 ring-emerald-200 dark:bg-emerald-900 dark:text-emerald-200'
  if (requestType === 'ws_v2') return 'bg-violet-50 text-violet-700 ring-violet-200 dark:bg-violet-900 dark:text-violet-200'
  if (requestType === 'stream') return 'bg-sky-50 text-sky-700 ring-sky-200 dark:bg-blue-900 dark:text-blue-200'
  if (requestType === 'sync') return 'bg-gray-100 text-gray-600 ring-gray-200 dark:bg-gray-700 dark:text-gray-200'
  return 'bg-amber-50 text-amber-700 ring-amber-200 dark:bg-amber-900 dark:text-amber-200'
}

const getRequestTypeIcon = (row: AdminUsageLog): 'shield' | 'play' | 'swap' | 'bolt' | 'sync' | 'questionCircle' => {
  const requestType = resolveUsageRequestType(row)
  if (requestType === 'cyber') return 'shield'
  if (requestType === 'live') return 'play'
  if (requestType === 'ws_v2') return 'swap'
  if (requestType === 'stream') return 'bolt'
  if (requestType === 'sync') return 'sync'
  return 'questionCircle'
}



const formatUserAgent = (ua: string): string => {
  return ua
}

const hasRecordedDuration = (ms: number | null | undefined): ms is number =>
  ms != null && Number.isFinite(ms) && ms >= 0

const getLatencyBarClass = (row: AdminUsageLog): string | string[] => {
  const first = hasRecordedDuration(row.first_token_ms) ? firstTokenSeverity(row.first_token_ms) : null
  const total = hasRecordedDuration(row.duration_ms) ? durationSeverity(row.duration_ms) : null
  if (first && total) {
    return ['bg-gradient-to-b from-40% to-60%', LATENCY_BAR_FROM_CLASSES[first], LATENCY_BAR_TO_CLASSES[total]]
  }
  if (first) return LATENCY_BAR_CLASSES[first]
  if (total) return LATENCY_BAR_CLASSES[total]
  return 'bg-gray-200 dark:bg-gray-700'
}

// 超过 1 分钟简化为 "Xm Ys"，免去人工换算（超过 1 小时再进位为 "Xh Ym"）
const formatDuration = (ms: number | null | undefined): string => {
  if (!hasRecordedDuration(ms)) return t('usage.performanceNotRecorded')
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(2)}s`
  const totalSec = Math.round(ms / 1000)
  if (totalSec < 3600) return `${Math.floor(totalSec / 60)}m ${totalSec % 60}s`
  return `${Math.floor(totalSec / 3600)}h ${Math.floor((totalSec % 3600) / 60)}m`
}

const TOOLTIP_GAP = 8
const VIEWPORT_MARGIN = 8

const isSideTooltip = (placement: TooltipPlacement): boolean => placement === 'right' || placement === 'left'

/**
 * Prefer the right side of the trigger, then the left side, and finally a horizontally
 * clamped position below/above it. Phones are too narrow for side placement, and cells
 * near the right edge of a wide table used to push the panel off screen.
 */
const placeTooltip = (anchor: DOMRect, panel?: DOMRect): TooltipPosition => {
  const middle = anchor.top + anchor.height / 2
  if (!panel) return { x: anchor.right + TOOLTIP_GAP, y: middle, placement: 'right' }
  const viewportWidth = window.innerWidth
  const viewportHeight = window.innerHeight
  const clampMiddle = (y: number) => Math.min(
    Math.max(y, VIEWPORT_MARGIN + panel.height / 2),
    Math.max(VIEWPORT_MARGIN + panel.height / 2, viewportHeight - VIEWPORT_MARGIN - panel.height / 2),
  )
  if (anchor.right + TOOLTIP_GAP + panel.width <= viewportWidth - VIEWPORT_MARGIN) {
    return { x: anchor.right + TOOLTIP_GAP, y: clampMiddle(middle), placement: 'right' }
  }
  if (anchor.left - TOOLTIP_GAP - panel.width >= VIEWPORT_MARGIN) {
    return { x: anchor.left - TOOLTIP_GAP - panel.width, y: clampMiddle(middle), placement: 'left' }
  }
  const x = Math.min(
    Math.max(anchor.left + anchor.width / 2 - panel.width / 2, VIEWPORT_MARGIN),
    Math.max(VIEWPORT_MARGIN, viewportWidth - VIEWPORT_MARGIN - panel.width),
  )
  const fitsBelow = anchor.bottom + TOOLTIP_GAP + panel.height <= viewportHeight - VIEWPORT_MARGIN
  if (fitsBelow || anchor.top - TOOLTIP_GAP - panel.height < VIEWPORT_MARGIN) {
    return { x, y: anchor.bottom + TOOLTIP_GAP, placement: 'below' }
  }
  return { x, y: anchor.top - TOOLTIP_GAP - panel.height, placement: 'above' }
}

const openTooltip = async (
  event: Event,
  row: AdminUsageLog,
  data: typeof tooltipData,
  position: typeof tooltipPosition,
  visible: typeof tooltipVisible,
  panel: typeof costTooltipRef,
) => {
  const target = event.currentTarget as HTMLElement | null
  if (!target) return
  const anchor = target.getBoundingClientRect()
  data.value = row
  position.value = placeTooltip(anchor)
  visible.value = true
  await nextTick()
  if (!visible.value || data.value !== row || !panel.value) return
  position.value = placeTooltip(anchor, panel.value.getBoundingClientRect())
}

// Touch screens follow a tap with a synthetic mouseleave, so a tap or click pins the panel
// until the next tap elsewhere, a scroll, Escape or focus leaving the trigger.
type TooltipKind = 'cost' | 'token'
const pinnedTooltip = ref<TooltipKind | null>(null)

// Cost tooltip functions
const showTooltip = (event: Event, row: AdminUsageLog) =>
  openTooltip(event, row, tooltipData, tooltipPosition, tooltipVisible, costTooltipRef)

const hideTooltip = () => {
  if (pinnedTooltip.value === 'cost') return
  tooltipVisible.value = false
  tooltipData.value = null
}

// Token tooltip functions
const showTokenTooltip = (event: Event, row: AdminUsageLog) =>
  openTooltip(event, row, tokenTooltipData, tokenTooltipPosition, tokenTooltipVisible, tokenTooltipRef)

const hideTokenTooltip = () => {
  if (pinnedTooltip.value === 'token') return
  tokenTooltipVisible.value = false
  tokenTooltipData.value = null
}

const closeTooltips = () => {
  pinnedTooltip.value = null
  hideTooltip()
  hideTokenTooltip()
}

const pinTooltip = (kind: TooltipKind, event: Event, row: AdminUsageLog) => {
  closeTooltips()
  pinnedTooltip.value = kind
  return kind === 'cost' ? showTooltip(event, row) : showTokenTooltip(event, row)
}

const closeTooltipsOnOutsidePointer = (event: Event) => {
  if (!pinnedTooltip.value) return
  const target = event.target as Element | null
  if (target?.closest?.('.usage-info-trigger')) return
  closeTooltips()
}

const closeTooltipsOnEscape = (event: KeyboardEvent) => {
  if (event.key === 'Escape') closeTooltips()
}

// Fixed-position panels would drift away from their trigger while the page or table scrolls.
const hideTooltipsOnScroll = () => {
  if (tooltipVisible.value || tokenTooltipVisible.value) closeTooltips()
}

onMounted(() => {
  window.addEventListener('scroll', hideTooltipsOnScroll, { capture: true, passive: true })
  document.addEventListener('pointerdown', closeTooltipsOnOutsidePointer, true)
  document.addEventListener('keydown', closeTooltipsOnEscape)
})
onBeforeUnmount(() => {
  window.removeEventListener('scroll', hideTooltipsOnScroll, { capture: true })
  document.removeEventListener('pointerdown', closeTooltipsOnOutsidePointer, true)
  document.removeEventListener('keydown', closeTooltipsOnEscape)
})
</script>

<style scoped>
.cache-hit-empty-mark {
  width: 1.25rem;
  height: 0.1875rem;
  flex: none;
  border-radius: 9999px;
  background: #e2d8b8;
}

.usage-table-frame {
  overflow: hidden;
  border: 1px solid transparent;
  border-radius: 1.25rem;
  background: var(--md-sys-color-surface);
}

.usage-table-frame :deep(.table-wrapper) {
  overflow: auto;
}

/* Inside a shared panel the table runs edge to edge. */
.usage-records--flat .usage-table-frame {
  border-radius: 0;
}

/* Phones render separate cards, so the frame steps back and lets each card stand on its own. */
@media (max-width: 767px) {
  .usage-table-frame {
    overflow: visible;
    border-radius: 0;
    background: transparent;
  }
}

/* Hairline row separators: shadows survive the console-wide transparent border reset. */
.usage-table-frame :deep(.table-body > tr:not(.data-empty-row) > td) {
  vertical-align: middle;
  box-shadow: inset 0 -1px 0 rgb(215 193 106 / 0.24);
}

.usage-table-frame :deep(.table-body > tr:last-child > td) {
  box-shadow: none;
}

.usage-table-frame :deep(.table-body .sticky-col) {
  background: color-mix(in srgb, var(--md-sys-color-surface) 78%, var(--md-sys-color-surface-container));
}

.usage-table-frame :deep(.table-body > tr:not(.data-empty-row):hover),
.usage-table-frame :deep(.table-body > tr:not(.data-empty-row):hover > .sticky-col) {
  background: color-mix(in srgb, var(--md-sys-color-primary) 9%, var(--md-sys-color-surface));
}

.usage-empty {
  font-size: 0.875rem;
  color: #d7caa2;
}

/* ---------- Shared chips ---------- */
.usage-chip {
  display: inline-flex;
  min-width: 0;
  max-width: 13rem;
  align-items: center;
  gap: 0.3125rem;
  padding: 0.1875rem 0.5rem;
  border-radius: 9999px;
  font-size: 0.6875rem;
  font-weight: 600;
  line-height: 1rem;
  white-space: nowrap;
}

.usage-group-chip {
  background: #fff8dc;
  color: #5b5238;
  box-shadow: inset 0 0 0 1px rgb(215 193 106 / 0.45);
}

.usage-key-chip {
  background: #fff3bd;
  color: #7e5600;
}

.usage-dot {
  display: inline-block;
  width: 0.4375rem;
  height: 0.4375rem;
  flex: none;
  border-radius: 9999px;
}

.usage-rate-chip {
  display: inline-flex;
  align-items: center;
  padding: 0 0.3125rem;
  border-radius: 0.3125rem;
  font-size: 0.625rem;
  font-weight: 700;
  line-height: 1rem;
  font-variant-numeric: tabular-nums;
}

.usage-effort-meter {
  display: inline-flex;
  height: 0.625rem;
  align-items: flex-end;
  gap: 1px;
}

.usage-effort-meter i {
  display: block;
  width: 0.1875rem;
  border-radius: 1px;
  background: currentColor;
  opacity: 0.22;
}

.usage-effort-meter i:nth-child(1) { height: 40%; }
.usage-effort-meter i:nth-child(2) { height: 60%; }
.usage-effort-meter i:nth-child(3) { height: 80%; }
.usage-effort-meter i:nth-child(4) { height: 100%; }
.usage-effort-meter i.is-on { opacity: 1; }

/* ---------- Desktop cells ---------- */
.usage-time {
  display: flex;
  flex-direction: column;
  gap: 0.125rem;
  line-height: 1.15;
  white-space: nowrap;
}

.usage-time-clock {
  font-size: 0.8125rem;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  color: var(--md-sys-color-on-surface);
}

.usage-time-day {
  font-size: 0.6875rem;
  font-variant-numeric: tabular-nums;
  color: #9b8c62;
}

.usage-key {
  display: inline-flex;
  min-width: 0;
  max-width: 10rem;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.8125rem;
  font-weight: 600;
  color: var(--md-sys-color-on-surface);
}

.usage-key-icon {
  display: inline-flex;
  width: 1.375rem;
  height: 1.375rem;
  flex: none;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  background: #fff3bd;
  color: #976800;
}

.usage-model-mark {
  display: inline-flex;
  width: 1.625rem;
  height: 1.625rem;
  flex: none;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  background: #fff;
  box-shadow: inset 0 0 0 1px rgb(215 193 106 / 0.5), 0 1px 2px rgb(63 52 32 / 0.06);
}

.usage-model-mark + div {
  padding-top: 0.1875rem;
}

.usage-model-mark--card {
  width: 2.25rem;
  height: 2.25rem;
  border-radius: 0.75rem;
}

.usage-model-name {
  font-size: 0.8125rem;
  font-weight: 600;
  line-height: 1.25rem;
  color: var(--md-sys-color-on-surface);
}

.usage-endpoint {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.375rem;
  font-size: 0.6875rem;
  color: #5b5238;
}

.usage-endpoint .truncate {
  padding: 0.125rem 0.375rem;
  border-radius: 0.375rem;
  background: #faf6e8;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.usage-endpoint-label {
  flex: none;
  font-size: 0.625rem;
  font-weight: 600;
  color: #9b8c62;
}

.usage-image-mark {
  display: inline-flex;
  width: 1.375rem;
  height: 1.375rem;
  flex: none;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  background: #fce7f3;
  color: #db2777;
}

.usage-token-mix {
  display: flex;
  width: 100%;
  max-width: 11rem;
  height: 0.25rem;
  gap: 2px;
  overflow: hidden;
  border-radius: 9999px;
  background: #f1ead2;
}

.usage-token-mix > span {
  min-width: 3px;
  flex-basis: 0;
  border-radius: 9999px;
}

.usage-hit-bar {
  position: relative;
  display: block;
  width: 3rem;
  height: 0.1875rem;
  overflow: hidden;
  border-radius: 9999px;
  background: #f1ead2;
}

.usage-hit-bar > span {
  position: absolute;
  inset: 0 auto 0 0;
  border-radius: inherit;
  background: currentColor;
}

.usage-cost {
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  color: #15803d;
}

.usage-info-trigger {
  display: inline-flex;
  width: 1.125rem;
  height: 1.125rem;
  flex: none;
  align-items: center;
  justify-content: center;
  border-radius: 9999px;
  background: #faf6e8;
  color: #9b8c62;
  cursor: help;
  transition: background-color 150ms ease, color 150ms ease;
}

.usage-info-trigger:hover,
.usage-info-trigger:focus-visible {
  background: #fff3bd;
  color: #976800;
}

.usage-tooltip-panel {
  max-width: calc(100vw - 1rem);
}

/* ---------- Phone cards ---------- */
.usage-records :deep(.data-mobile-card) {
  padding: 0.875rem 0.9375rem;
  border-radius: 1.25rem;
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.usage-card {
  min-width: 0;
}

/* The price keeps its corner; only very narrow phones move it below the title. */
.usage-card-head {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  gap: 0.375rem 0.75rem;
}

.usage-card-heading {
  min-width: 7rem;
  flex: 1 1 0%;
}

.usage-card-price {
  flex: none;
  margin-left: auto;
  text-align: right;
}

.usage-card-title {
  display: -webkit-box;
  overflow: hidden;
  font-size: 0.9375rem;
  font-weight: 700;
  line-height: 1.3125rem;
  overflow-wrap: anywhere;
  color: var(--md-sys-color-on-surface);
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}

.usage-card-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 0 0.5rem;
  margin-top: 0.125rem;
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
  color: #9b8c62;
}

.usage-card-cost {
  font-size: 0.9375rem;
  font-weight: 800;
  font-variant-numeric: tabular-nums;
  color: #15803d;
}

.usage-card .usage-info-trigger {
  width: 1.5rem;
  height: 1.5rem;
}

.usage-card .usage-chip {
  max-width: 100%;
}

.usage-card-panel {
  padding: 0.625rem 0.75rem;
  border-radius: 0.875rem;
  background: color-mix(in srgb, var(--md-sys-color-surface-container) 42%, var(--md-sys-color-surface));
}

.usage-card .usage-token-mix {
  max-width: none;
  height: 0.375rem;
}

.usage-stat {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.125rem;
}

.usage-stat-label {
  display: inline-flex;
  align-items: center;
  gap: 0.3125rem;
  overflow: hidden;
  font-size: 0.6875rem;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: #9b8c62;
}

.usage-stat-value {
  font-size: 0.9375rem;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  color: var(--md-sys-color-on-surface);
}

.usage-card-perf {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.25rem 0.75rem;
  font-size: 0.75rem;
  color: #9b8c62;
}

.usage-card-perf b {
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.usage-perf-dot {
  width: 0.5rem;
  height: 0.5rem;
  flex: none;
  border-radius: 9999px;
}

.usage-card-footer {
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem 0.875rem;
  padding-top: 0.625rem;
  font-size: 0.6875rem;
  color: #9b8c62;
  box-shadow: inset 0 1px 0 rgb(215 193 106 / 0.28);
}

.usage-card-footer-item {
  display: inline-flex;
  min-width: 0;
  max-width: 100%;
  align-items: center;
  gap: 0.3125rem;
}
</style>
