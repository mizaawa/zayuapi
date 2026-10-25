import { describe, expect, it } from 'vitest'
import source from '../CreateAccountModal.vue?raw'

describe('CreateAccountModal API Key-only onboarding', () => {
  it('keeps the official xAI URL and API Key input', () => {
    expect(source).toContain("platform === 'grok'")
    expect(source).toContain("? 'https://api.x.ai/v1'")
    expect(source).toContain("? 'xai-...'")
    expect(source).toContain('data-testid="account-type-api-key"')
  })

  it('removes alternate authorization handlers and components', () => {
    for (const removed of ['OAuthAuthorizationFlow', 'useAccountOAuth', 'useOpenAIOAuth', 'useGeminiOAuth', 'useAntigravityOAuth', 'useGrokOAuth', 'importCodexSession', 'createOpenAICodexPAT', 'service_account_json', 'bedrockAccessKeyId', 'accountCategory']) {
      expect(source).not.toContain(removed)
    }
    expect(source).toContain("type: 'apikey' as const")
  })
})
