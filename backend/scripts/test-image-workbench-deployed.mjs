#!/usr/bin/env node

// Runs against an actual deployed application and can incur provider charges.
// Required environment: IMAGE_WORKBENCH_EMAIL, IMAGE_WORKBENCH_PASSWORD,
// IMAGE_WORKBENCH_REFERENCE, IMAGE_WORKBENCH_MODEL, IMAGE_WORKBENCH_KEY_NAME,
// IMAGE_WORKBENCH_OUTPUT_DIR. IMAGE_WORKBENCH_BASE_URL defaults to localhost:3030.
// Optional: IMAGE_WORKBENCH_SIZE, IMAGE_WORKBENCH_QUALITY,
// IMAGE_WORKBENCH_COUNT, IMAGE_WORKBENCH_PROMPT, IMAGE_WORKBENCH_TIMEOUT_SECONDS,
// IMAGE_WORKBENCH_POLL_SECONDS. Use a dedicated user without concurrent spending.
// External instances require IMAGE_WORKBENCH_ALLOW_EXTERNAL=1. No credentials,
// upstream URLs, prompts, response bodies or image base64 are written to reports.

import { createHash, randomUUID } from 'node:crypto'
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises'
import { isIP } from 'node:net'
import { join, resolve } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'

const started = Date.now()
const report = {
  schema_version: 1,
  started_at: new Date(started).toISOString(),
  status: 'running',
  stage: 'configuration',
  transitions: [],
  downloads: [],
  requests: [],
}
let outputDirectory
let baseURL
let accessToken
let apiKey
let email
let userID

class RegressionError extends Error {}

function check(condition, message) {
  if (!condition) throw new RegressionError(message)
}

function required(name) {
  const value = process.env[name]
  check(typeof value === 'string' && value.trim() !== '', `${name} is required`)
  return value
}

function integer(name, fallback, min, max) {
  const value = Number(process.env[name] ?? fallback)
  check(Number.isSafeInteger(value) && value >= min && value <= max, `${name} is out of range`)
  return value
}

function isLoopback(host) {
  host = host.replace(/^\[|\]$/g, '').toLowerCase()
  return host === 'localhost' || host === '::1' ||
    (isIP(host) === 4 && host.split('.')[0] === '127')
}

function imageFormat(bytes) {
  if (bytes.length >= 8 && bytes.subarray(0, 8).equals(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]))) {
    return { mime: 'image/png', extension: 'png' }
  }
  if (bytes.length >= 4 && bytes[0] === 255 && bytes[1] === 216 && bytes[2] === 255) {
    return { mime: 'image/jpeg', extension: 'jpg' }
  }
  if (bytes.length >= 12 && bytes.toString('ascii', 0, 4) === 'RIFF' && bytes.toString('ascii', 8, 12) === 'WEBP') {
    return { mime: 'image/webp', extension: 'webp' }
  }
  return null
}

function sha256(bytes) {
  return createHash('sha256').update(bytes).digest('hex')
}

async function saveReport() {
  if (!outputDirectory) return
  await writeFile(join(outputDirectory, 'report.json'), JSON.stringify(report, null, 2) + '\n', { mode: 0o600 })
}

async function readLimited(response, limit) {
  const reader = response.body?.getReader()
  check(reader, 'Response has no body')
  const chunks = []
  let size = 0
  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      size += value.length
      check(size <= limit, 'Response exceeds the test download limit')
      chunks.push(value)
    }
    return Buffer.concat(chunks, size)
  } finally {
    await reader.cancel().catch(() => {})
    reader.releaseLock()
  }
}

async function request(path, { method = 'GET', body, auth = 'session', expected = 200, binary = false } = {}) {
  const headers = { Accept: binary ? 'image/*' : 'application/json' }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (auth === 'session') {
    headers.Authorization = `Bearer ${accessToken}`
    headers['X-User-UI-Request'] = '1'
  } else if (auth === 'key') {
    headers.Authorization = `Bearer ${apiKey}`
    headers['X-Sub2API-User-Email'] = email
    if (method === 'POST') headers['X-Sub2API-Image-Workbench'] = 'true'
  }
  const entry = { method, path, stage: report.stage, elapsed_ms: Date.now() - started }
  report.requests.push(entry)
  let response
  try {
    response = await fetch(new URL(path, baseURL), {
      method, headers, body: body === undefined ? undefined : JSON.stringify(body),
      redirect: 'error', cache: 'no-store', signal: AbortSignal.timeout(30_000),
    })
    entry.status = response.status
    check(response.status === expected, `HTTP ${response.status} during ${report.stage}`)
    const bytes = await readLimited(response, binary ? 64 * 1024 * 1024 : 8 * 1024 * 1024)
    entry.response_bytes = bytes.length
    if (binary) return { bytes, contentType: response.headers.get('content-type')?.split(';')[0].trim().toLowerCase() }
    let value
    try { value = JSON.parse(bytes.toString('utf8')) } catch { throw new RegressionError('Server returned invalid JSON') }
    if (value && typeof value === 'object' && 'code' in value) {
      check(value.code === 0, `Application rejected request during ${report.stage}`)
      return value.data
    }
    return value
  } catch (error) {
    if (error instanceof RegressionError) throw error
    throw new RegressionError(`Network, redirect or timeout failure during ${report.stage}`)
  } finally {
    await response?.body?.cancel().catch(() => {})
  }
}

async function currentUser() {
  const user = await request('/api/v1/auth/me')
  check(user?.id === userID && user.role === 'user', 'Session must remain the same ordinary user')
  check(typeof user.balance === 'number' && Number.isFinite(user.balance), 'User balance is not numeric')
  return user
}

function snapshot(user) {
  return { balance: user.balance, frozen_balance: typeof user.frozen_balance === 'number' ? user.frozen_balance : null }
}

function recordTask(task) {
  check(['queued', 'processing', 'completed', 'failed'].includes(task.status), 'Task has an unknown status')
  report.task.status = task.status
  report.task.http_status = typeof task.http_status === 'number' ? task.http_status : null
  if (report.transitions.at(-1)?.status !== task.status) {
    report.transitions.push({ status: task.status, elapsed_ms: Date.now() - started })
  }
}

async function run() {
  outputDirectory = join(resolve(required('IMAGE_WORKBENCH_OUTPUT_DIR')), `workbench-${new Date().toISOString().replace(/[:.]/g, '-')}-${randomUUID().slice(0, 8)}`)
  await mkdir(outputDirectory, { recursive: true, mode: 0o700 })
  report.output_directory = outputDirectory
  try { baseURL = new URL(process.env.IMAGE_WORKBENCH_BASE_URL || 'http://127.0.0.1:3030') } catch { throw new RegressionError('IMAGE_WORKBENCH_BASE_URL is invalid') }
  check(['http:', 'https:'].includes(baseURL.protocol) && !baseURL.username && !baseURL.password, 'Instance URL must be HTTP(S) without credentials')
  check(baseURL.pathname === '/' && !baseURL.search && !baseURL.hash, 'Instance URL must contain only an origin')
  check(isLoopback(baseURL.hostname) || process.env.IMAGE_WORKBENCH_ALLOW_EXTERNAL === '1', 'External instances require IMAGE_WORKBENCH_ALLOW_EXTERNAL=1')
  report.instance = baseURL.origin
  const loginEmail = required('IMAGE_WORKBENCH_EMAIL').trim()
  const password = required('IMAGE_WORKBENCH_PASSWORD')
  const referencePath = resolve(required('IMAGE_WORKBENCH_REFERENCE'))
  const model = required('IMAGE_WORKBENCH_MODEL').trim()
  const keyName = required('IMAGE_WORKBENCH_KEY_NAME').trim()
  const size = process.env.IMAGE_WORKBENCH_SIZE || '1024x1024'
  const quality = process.env.IMAGE_WORKBENCH_QUALITY || 'low'
  // The workbench submits one queued task for every requested output image.
  const count = 1
  const timeoutMS = integer('IMAGE_WORKBENCH_TIMEOUT_SECONDS', 900, 30, 3600) * 1000
  const pollMS = integer('IMAGE_WORKBENCH_POLL_SECONDS', 3, 1, 30) * 1000
  check(['auto', 'low', 'medium', 'high'].includes(quality), 'Image quality must be auto, low, medium or high')
  check(size === 'auto' || /^\d{2,5}x\d{2,5}$/.test(size), 'Image size must be auto or WIDTHxHEIGHT')
  const referenceStat = await stat(referencePath)
  check(referenceStat.isFile() && referenceStat.size > 0 && referenceStat.size <= 50 * 1024 * 1024, 'Reference must be a nonempty file no larger than 50 MiB')
  const reference = await readFile(referencePath)
  const referenceFormat = imageFormat(reference)
  check(referenceFormat, 'Reference signature must be PNG, JPEG or WebP')
  report.reference = { bytes: reference.length, content_type: referenceFormat.mime, sha256: sha256(reference) }
  report.parameters = { model, size, quality, n: count, reference_count: 1 }

  report.stage = 'login'
  const login = await request('/api/v1/auth/login', { method: 'POST', auth: 'none', body: { email: loginEmail, password } })
  check(login?.user?.role === 'user', 'A normal user account is required; admin sessions are rejected')
  check(typeof login.access_token === 'string' && login.access_token.length > 0, 'Login did not return a session token; check login policy or MFA')
  check(typeof login.user.email === 'string' && Number.isSafeInteger(login.user.id), 'Login returned an invalid user')
  accessToken = login.access_token
  email = login.user.email
  userID = login.user.id
  report.user = { id: userID, role: 'user' }

  report.stage = 'read_keys'
  const matches = []
  for (let page = 1; ; page++) {
    check(page <= 1000, 'API key pagination exceeded 1000 pages')
    const keys = await request(`/api/v1/keys?page=${page}&page_size=100&status=active`)
    check(Array.isArray(keys?.items) && Number.isSafeInteger(keys.pages), 'Invalid API key list')
    matches.push(...keys.items.filter(key => key.name === keyName))
    if (page >= keys.pages || keys.items.length === 0) break
  }
  check(matches.length === 1, 'Expected exactly one active API key with IMAGE_WORKBENCH_KEY_NAME')
  const key = matches[0]
  check(key.user_id === userID && typeof key.key === 'string' && key.key.length > 0, 'API key does not belong to this user or is unavailable')
  check(!key.expires_at || Date.parse(key.expires_at) > Date.now(), 'Selected API key is expired')
  check(key.group?.allow_image_generation === true && key.group.image_workbench_enabled === true, 'Selected API key group must enable image generation and the workbench')
  check(['openai', 'custom', 'grok', 'composite'].includes(key.group.platform), 'Selected group does not support the image workbench')
  apiKey = key.key
  report.api_key = { id: key.id, group_id: key.group.id, platform: key.group.platform, subscription_type: key.group.subscription_type }

  report.stage = 'read_models'
  const models = await request('/v1/models?workbench=image', { auth: 'key' })
  check(Array.isArray(models?.data) && models.data.some(item => item.id === model), 'Expected model is missing from the real workbench model list')
  report.model_list_verified = true

  report.stage = 'read_capabilities'
  const initialTasks = await request('/api/v1/image-workbench/tasks')
  check(initialTasks?.enabled === true && Array.isArray(initialTasks.tasks), 'Deployed image workbench is not enabled')
  check(Number.isFinite(initialTasks.max_image_size_mb) && (initialTasks.max_image_size_mb === 0 || reference.length <= initialTasks.max_image_size_mb * 1024 * 1024), 'Reference exceeds deployed workbench limit')
  check(Number.isFinite(initialTasks.max_reference_images) && initialTasks.max_reference_images >= 1, 'Deployed workbench does not accept reference images')
  report.capabilities = { max_concurrent: initialTasks.max_concurrent, max_image_size_mb: initialTasks.max_image_size_mb, max_reference_images: initialTasks.max_reference_images }
  report.stage = 'balance_before'
  const before = await currentUser()
  report.balance = { before: snapshot(before), expectation: key.group.subscription_type === 'subscription' ? 'subscription_balance_unchanged' : 'balance_decreases' }
  check(before.run_mode !== 'simple', 'Billing regression requires the standard deployment mode')
  const usageBefore = typeof key.quota_used === 'number' ? key.quota_used : null

  report.stage = 'submit_reference_image'
  const accepted = await request('/v1/images/edits/async', {
    method: 'POST', auth: 'key', expected: 202,
    body: {
      model, size, quality, n: count,
      prompt: process.env.IMAGE_WORKBENCH_PROMPT || 'Change the main object to green. Preserve the composition and produce one edited image.',
      images: [{ image_url: `data:${referenceFormat.mime};base64,${reference.toString('base64')}` }],
    },
  })
  check(typeof accepted?.id === 'string' && /^imgtask_[a-f0-9]{32}$/.test(accepted.id), 'Submission did not return a valid task ID')
  check(accepted.status === 'queued', 'Workbench submission did not enter the real queue')
  check(accepted.workbench?.model === model && accepted.workbench.api_key_id === key.id, 'Submitted task metadata does not match the model or API key')
  report.task = { id: accepted.id }
  recordTask(accepted)
  await saveReport()
  report.stage = 'balance_after_admission'
  report.balance.after_admission = snapshot(await currentUser())

  report.stage = 'poll_user_tasks'
  const deadline = Date.now() + timeoutMS
  let completed
  while (Date.now() < deadline) {
    const listed = await request('/api/v1/image-workbench/tasks')
    check(Array.isArray(listed?.tasks), 'Invalid user task list')
    const task = listed.tasks.find(item => item.id === accepted.id)
    if (task) {
      recordTask(task)
      check(task.workbench?.api_key_id === key.id && task.workbench.model === model, 'Polled task metadata changed')
      if (task.status === 'failed') {
        const publicCodes = new Set(['generation_failed', 'request_rejected', 'rate_limited', 'timeout', 'authorization_failed', 'invalid_image_input', 'content_rejected', 'unsupported_size', 'invalid_parameters', 'model_unavailable', 'insufficient_balance'])
        report.task.error_code = publicCodes.has(task.error?.code) ? task.error.code : 'unclassified'
        throw new RegressionError('Real workbench task failed; inspect task HTTP status and server logs')
      }
      if (task.status === 'completed') { completed = task; break }
    }
    await delay(pollMS)
  }
  check(completed, 'Timed out waiting for the real workbench task to complete')
  check(Array.isArray(completed.result?.data) && completed.result.data.length === count, 'Completed task has an unexpected number of result images')
  report.task.created_at = completed.created_at
  report.task.started_at = completed.started_at
  report.task.completed_at = completed.completed_at
  await saveReport()

  report.stage = 'download_images'
  for (let index = 0; index < count; index++) {
    // Construct the authenticated same-origin route, never follow provider URLs.
    const download = await request(`/api/v1/image-workbench/tasks/${accepted.id}/images/${index}`, { binary: true })
    const format = imageFormat(download.bytes)
    check(format && format.mime === download.contentType, 'Downloaded image signature and Content-Type must agree')
    const file = `image-${index + 1}.${format.extension}`
    await writeFile(join(outputDirectory, file), download.bytes, { mode: 0o600, flag: 'wx' })
    report.downloads.push({ file, bytes: download.bytes.length, content_type: format.mime, sha256: sha256(download.bytes) })
  }

  report.stage = 'balance_after_completion'
  let after
  const billingDeadline = Date.now() + 30_000
  let previousBalance
  let stableSince = Date.now()
  do {
    after = await currentUser()
    if (after.balance !== previousBalance) stableSince = Date.now()
    previousBalance = after.balance
    if ((key.group.subscription_type === 'subscription' || after.balance < before.balance) && Date.now() - stableSince >= 4000) break
    await delay(2000)
  } while (Date.now() < billingDeadline)
  report.balance.after_completion = snapshot(after)
  report.balance.decrease = Number((before.balance - after.balance).toFixed(12))
  check(after.balance >= 0, 'User balance became negative')
  if (key.group.subscription_type === 'subscription') {
    check(Math.abs(after.balance - before.balance) < 1e-9, 'Subscription request unexpectedly changed the user balance')
  } else {
    check(after.balance < before.balance, 'Completed paid image did not reduce the user balance within 30 seconds')
  }
  const keyAfter = await request(`/api/v1/keys/${key.id}`)
  check(keyAfter.id === key.id && keyAfter.user_id === userID, 'API key ownership changed')
  report.balance.api_key_quota_used_before = usageBefore
  report.balance.api_key_quota_used_after = typeof keyAfter.quota_used === 'number' ? keyAfter.quota_used : null
  report.balance.verified = true
  report.stage = 'complete'
  report.status = 'passed'
}

try {
  await run()
} catch (error) {
  report.status = 'failed'
  report.error = error instanceof RegressionError ? error.message : 'Local file or configuration operation failed'
  process.exitCode = 1
  if (accessToken && report.balance) {
    const failedStage = report.stage
    report.stage = 'balance_after_failure'
    try {
      report.balance.after_failure = snapshot(await currentUser())
    } catch {
      report.balance.failure_snapshot_unavailable = true
    }
    report.stage = failedStage
  }
} finally {
  report.finished_at = new Date().toISOString()
  report.duration_ms = Date.now() - started
  try { await saveReport() } catch {
    report.status = 'failed'
    report.report_write_failed = true
    process.exitCode = 1
  }
  console.log(JSON.stringify(report, null, 2))
}
