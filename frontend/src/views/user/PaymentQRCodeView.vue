<template>
  <AppLayout>
    <div class="mx-auto flex max-w-md flex-col items-center space-y-6 py-8">
      <p v-if="loading" role="status">{{ t('common.loading') }}</p>
      <div v-else-if="loadError" class="text-center" role="alert">
        <p class="text-red-500">{{ loadError }}</p>
        <button class="btn btn-primary mt-4" @click="router.push('/purchase')">{{ t('payment.result.backToRecharge') }}</button>
      </div>
      <template v-else>
      <h2 class="text-xl font-semibold text-gray-900 dark:text-white">
        {{ qrUrl ? scanTitle : t('payment.qr.payInNewWindow') }}
      </h2>
      <div v-if="qrUrl && !expired" class="rounded-2xl bg-white p-6 shadow-lg dark:bg-dark-800">
        <canvas ref="qrCanvas" class="mx-auto"></canvas>
      </div>
      <!-- Scan prompt for QR code -->
      <p v-if="qrUrl && !expired && scanHint" class="text-center text-sm text-gray-500 dark:text-gray-400">
        {{ scanHint }}
      </p>
      <div v-if="expired" class="text-center">
        <p class="text-lg font-medium text-red-500">{{ t('payment.qr.expired') }}</p>
        <button class="btn btn-primary mt-4" @click="router.push('/purchase')">{{ t('payment.result.backToRecharge') }}</button>
      </div>
      <div v-else class="text-center">
        <p class="text-sm text-gray-500 dark:text-gray-400">{{ qrUrl ? t('payment.qr.expiresIn') : t('payment.qr.payInNewWindowHint') }}</p>
        <p class="mt-1 text-2xl font-bold tabular-nums text-gray-900 dark:text-white">{{ countdownDisplay }}</p>
        <p class="mt-2 text-sm text-gray-400 dark:text-gray-500">{{ t('payment.qr.waitingPayment') }}</p>
      </div>
      <a v-if="payUrl && !qrUrl && !expired" :href="payUrl" target="_blank" rel="noopener noreferrer"
        class="btn btn-primary w-full py-3">
        {{ t('payment.qr.openPayWindow') }}
      </a>
      <!-- Cancel button -->
      <button v-if="!expired && orderId" class="btn btn-secondary w-full" :disabled="cancelling" @click="handleCancel">
        {{ cancelling ? t('common.processing') : t('payment.qr.cancelOrder') }}
      </button>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import { usePaymentStore } from '@/stores/payment'
import { paymentAPI } from '@/api/payment'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { useAppStore } from '@/stores'
import { isBuiltInAlipayMethod, isBuiltInWxpayMethod } from '@/components/payment/providerConfig'
import { sanitizePaymentUrl } from '@/components/payment/paymentUrl'
import QRCode from 'qrcode'
import alipayIcon from '@/assets/icons/alipay.svg'
import wxpayIcon from '@/assets/icons/wxpay.svg'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const paymentStore = usePaymentStore()
const appStore = useAppStore()

const qrCanvas = ref<HTMLCanvasElement | null>(null)
const qrUrl = ref('')
const payUrl = ref('')
const orderId = ref(0)
const remainingSeconds = ref(0)
const expired = ref(false)
const cancelling = ref(false)
const paymentType = ref('')
const loading = ref(true)
const loadError = ref('')
let disposed = false

let pollTimer: ReturnType<typeof setInterval> | null = null
let countdownTimer: ReturnType<typeof setInterval> | null = null

const countdownDisplay = computed(() => {
  const m = Math.floor(remainingSeconds.value / 60)
  const s = remainingSeconds.value % 60
  return m.toString().padStart(2, '0') + ':' + s.toString().padStart(2, '0')
})

const isAlipay = computed(() => isBuiltInAlipayMethod(paymentType.value))
const isWxpay = computed(() => isBuiltInWxpayMethod(paymentType.value))

const scanTitle = computed(() => {
  if (isAlipay.value) return t('payment.qr.scanAlipay')
  if (isWxpay.value) return t('payment.qr.scanWxpay')
  return t('payment.qr.scanToPay')
})

const scanHint = computed(() => {
  if (isAlipay.value) return t('payment.qr.scanAlipayHint')
  if (isWxpay.value) return t('payment.qr.scanWxpayHint')
  return ''
})

function getLogoForType(): string | null {
  if (isAlipay.value) return alipayIcon
  if (isWxpay.value) return wxpayIcon
  return null
}

async function renderQR() {
  await nextTick()
  if (!qrCanvas.value || !qrUrl.value) return

  // Use medium error correction to support logo overlay while keeping QR code scannable
  const logoSrc = getLogoForType()
  await QRCode.toCanvas(qrCanvas.value, qrUrl.value, {
    width: 256,
    margin: 2,
    errorCorrectionLevel: logoSrc ? 'M' : 'L',
  })

  if (!logoSrc) return

  // Draw logo in center of QR code
  const canvas = qrCanvas.value
  const ctx = canvas.getContext('2d')
  if (!ctx) return

  const img = new Image()
  img.src = logoSrc
  img.onload = () => {
    const logoSize = 48
    const x = (canvas.width - logoSize) / 2
    const y = (canvas.height - logoSize) / 2
    // White background with rounded corners
    const pad = 5
    ctx.fillStyle = '#FFFFFF'
    ctx.beginPath()
    const r = 6
    ctx.moveTo(x - pad + r, y - pad)
    ctx.arcTo(x + logoSize + pad, y - pad, x + logoSize + pad, y + logoSize + pad, r)
    ctx.arcTo(x + logoSize + pad, y + logoSize + pad, x - pad, y + logoSize + pad, r)
    ctx.arcTo(x - pad, y + logoSize + pad, x - pad, y - pad, r)
    ctx.arcTo(x - pad, y - pad, x + logoSize + pad, y - pad, r)
    ctx.fill()
    // Draw logo
    ctx.drawImage(img, x, y, logoSize, logoSize)
  }
}

let pollInFlight = false
async function pollStatus() {
  if (!orderId.value) return
  // 防重入：接口响应慢于 3 秒轮询间隔时避免并发重叠请求与重复跳转。
  if (pollInFlight) return
  pollInFlight = true
  try {
    const order = await paymentStore.pollOrderStatus(orderId.value)
    if (!order) return
    // 定时器已被 cleanup 清除时不再执行终态跳转（响应可能在 cleanup 后才回来）。
    if (!pollTimer) return
    if (order.status === 'COMPLETED' || order.status === 'PAID' || order.status === 'RECHARGING') {
      cleanup()
      router.push({ path: '/payment/result', query: { order_id: String(orderId.value), status: 'success' } })
    } else if (order.status === 'EXPIRED' || order.status === 'CANCELLED' || order.status === 'FAILED') {
      cleanup()
      expired.value = true
    }
  } finally {
    pollInFlight = false
  }
}

function startCountdown(seconds: number) {
  remainingSeconds.value = Math.max(0, seconds)
  if (remainingSeconds.value <= 0) {
    expired.value = true
    return
  }
  countdownTimer = setInterval(() => {
    remainingSeconds.value--
    if (remainingSeconds.value <= 0) {
      expired.value = true
      cleanup()
    }
  }, 1000)
}

async function handleCancel() {
  if (!orderId.value || cancelling.value) return
  cancelling.value = true
  try {
    await paymentAPI.cancelOrder(orderId.value)
    cleanup()
    router.push('/purchase')
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  } finally {
    cancelling.value = false
  }
}

function cleanup() {
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
  if (countdownTimer) { clearInterval(countdownTimer); countdownTimer = null }
}

watch(qrUrl, () => renderQR())

onMounted(async () => {
  const rawID = route.query.order_id
  const requestedID = typeof rawID === 'string' && /^[1-9]\d*$/.test(rawID) ? Number(rawID) : 0
  if (!Number.isSafeInteger(requestedID) || requestedID <= 0) {
    loadError.value = t('payment.errors.NOT_FOUND')
    loading.value = false
    return
  }

  try {
    // All payment contents must come from this authenticated, owner-checked lookup.
    // Query parameters are untrusted and must never choose the payee or expiry.
    const { data: order } = await paymentAPI.getOrder(requestedID)
    if (disposed) return
    if (!order || order.id !== requestedID) throw new Error('Invalid payment order')
    orderId.value = order.id
    if (['COMPLETED', 'PAID', 'RECHARGING'].includes(order.status)) {
      await router.replace({ path: '/payment/result', query: { order_id: String(order.id), status: 'success' } })
      return
    }
    if (order.status !== 'PENDING') {
      expired.value = true
      return
    }

    const expiresAt = Date.parse(order.expires_at)
    if (!Number.isFinite(expiresAt)) throw new Error('Invalid payment expiry')
    qrUrl.value = sanitizePaymentUrl(order.qr_code)
    payUrl.value = sanitizePaymentUrl(order.pay_url)
    paymentType.value = order.payment_type
    if (!qrUrl.value && !payUrl.value) {
      loadError.value = t('payment.errors.PAYMENT_GATEWAY_ERROR')
      return
    }
    startCountdown(Math.floor((expiresAt - Date.now()) / 1000))
    if (!expired.value) pollTimer = setInterval(pollStatus, 3000)
  } catch (err: unknown) {
    if (!disposed) loadError.value = extractI18nErrorMessage(err, t, 'payment.errors', t('common.error'))
  } finally {
    if (!disposed) loading.value = false
  }
})

onUnmounted(() => { disposed = true; cleanup() })
</script>
