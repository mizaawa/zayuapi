<template>
  <div class="auth-entry">
    <header class="entry-bar">
      <nav class="entry-nav">
        <RouterLink to="/" class="entry-brand" :title="t('auth.entry.home')">
          <img :src="siteLogo || '/logo.svg'" alt="" width="40" height="40" />
          <span>{{ siteName }}</span>
        </RouterLink>
        <div class="entry-tools">
          <LocaleSwitcher />
          <a
            v-if="docsUrl"
            :href="docsUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="entry-docs"
            :aria-label="t('home.viewDocs')"
            :title="t('home.viewDocs')"
          >
            <Icon name="book" size="md" aria-hidden="true" />
          </a>
        </div>
      </nav>
    </header>

    <main class="entry-main">
      <section class="entry-panel" aria-labelledby="entry-title">
        <header class="entry-heading">
          <h1 id="entry-title">{{ t(entry === 'login' ? 'auth.signIn' : 'auth.createAccount') }}</h1>
          <RouterLink
            v-if="entry === 'register' || registrationOpen"
            :to="entry === 'login' ? '/register' : '/login'"
            class="entry-switch"
          >
            {{ t(entry === 'login' ? 'auth.signUp' : 'auth.signIn') }}
            <Icon name="arrowRight" size="sm" aria-hidden="true" />
          </RouterLink>
        </header>
        <slot />
      </section>
    </main>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'

defineProps<{
  entry: 'login' | 'register'
  siteName: string
  siteLogo: string
  docsUrl: string
  registrationOpen: boolean
}>()
const { t } = useI18n()
</script>

<style scoped>
.auth-entry {
  --entry-surface: var(--md-sys-color-surface-container);
  --entry-ink: theme('colors.gray.900');
  --entry-muted: theme('colors.gray.500');
  --entry-line: theme('colors.gray.200');
  --entry-accent-ink: theme('colors.primary.600');
  --entry-pad: clamp(1.25rem, 5vw, 2.5rem);
  min-height: 100vh;
  min-height: 100svh;
  display: flex;
  flex-direction: column;
  color: var(--entry-ink);
  background: radial-gradient(ellipse 46rem 32rem at 50% 36%, rgb(233 184 36 / 0.1), transparent 85%), linear-gradient(135deg, theme('colors.gray.50'), rgb(255 251 232 / 0.3), theme('colors.gray.100'));
}

.entry-bar { border-bottom: 1px solid var(--entry-line); }
.entry-nav {
  width: min(calc(100% - 3rem), 76rem);
  min-height: 4.5rem;
  margin-inline: auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}
.entry-brand, .entry-tools, .entry-docs, .entry-switch { display: inline-flex; align-items: center; }
.entry-brand { min-width: 0; min-height: 2.75rem; gap: .75rem; font-size: 1.0625rem; font-weight: 700; }
.entry-brand img { flex: none; width: 2.5rem; height: 2.5rem; object-fit: contain; border-radius: .875rem; }
.entry-brand span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.entry-tools { flex: none; gap: .25rem; }
.entry-docs, .entry-tools :deep(.relative > button) { min-width: 2.75rem; min-height: 2.75rem; justify-content: center; color: var(--entry-muted); }
.entry-docs { border-radius: .875rem; }
.entry-docs:hover { background: var(--md-sys-color-surface-container); }

.entry-main {
  flex: 1;
  display: grid;
  place-items: center;
  grid-template-columns: minmax(0, 1fr);
  padding: clamp(2rem, 6vh, 4rem) 1.25rem max(4rem, 10vh);
}
.entry-panel {
  width: min(100%, 30rem);
  min-width: 0;
  padding: var(--entry-pad);
  background: var(--entry-surface);
  border: 1px solid transparent;
  border-radius: 1.75rem;
  box-shadow: var(--theme-highlight), var(--theme-elevation-2), 0 1.75rem 4.5rem -3.5rem var(--md-sys-color-outline);
  overflow-wrap: anywhere;
}
.entry-heading { display: flex; align-items: center; justify-content: space-between; gap: 1rem; margin-bottom: clamp(1.5rem, 4vw, 2rem); }
.entry-heading h1 { font-size: clamp(1.875rem, 3.5vw, 2.25rem); line-height: 1.18; font-weight: 700; letter-spacing: -.035em; }
.entry-switch { flex: none; justify-content: center; gap: .375rem; min-height: 2.75rem; padding-inline: .875rem; border-radius: 999px; background: transparent; color: var(--entry-accent-ink); font-size: .875rem; font-weight: 600; }
.entry-switch:hover { color: theme('colors.primary.500'); }

.entry-panel :deep(.entry-form) { display: grid; grid-template-columns: minmax(0, 1fr); gap: 1.2rem .875rem; }
.entry-panel :deep(.input-label) { color: theme('colors.gray.700'); font-weight: 600; }
.entry-panel :deep(.auth-input) {
  min-height: 3rem;
  border-radius: .875rem;
}
.entry-panel :deep(.entry-reveal) { position: absolute; inset: 0 0 0 auto; display: grid; place-items: center; width: 2.75rem; border-radius: .875rem; color: theme('colors.gray.400'); }
.entry-panel :deep(.entry-reveal:hover) { color: theme('colors.gray.600'); }
.entry-panel :deep(.entry-link) { color: var(--entry-accent-ink); font-weight: 600; text-underline-offset: .2em; }
.entry-panel :deep(.entry-link:hover) { color: theme('colors.primary.500'); text-decoration: underline; }
.entry-panel :deep(.entry-submit) { width: 100%; min-height: 3rem; border-radius: .875rem; font-size: .975rem; }
.entry-panel :deep(.entry-divider) { display: flex; align-items: center; gap: .875rem; color: var(--entry-muted); font-size: .75rem; }
.entry-panel :deep(.entry-divider)::before, .entry-panel :deep(.entry-divider)::after { content: ''; flex: 1; height: 1px; background: var(--entry-line); }
.entry-panel :deep(.entry-providers) { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 11rem), 1fr)); gap: .7rem; }
.entry-panel :deep(.entry-provider-pair) { grid-column: 1 / -1; }
.entry-panel :deep(.entry-provider-pair > .grid) { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: .7rem; }
.entry-panel :deep(.btn-secondary) { min-height: 2.75rem; border-radius: .875rem; }
.auth-entry :deep(:is(a, button):focus-visible) { outline: 2px solid var(--entry-accent-ink); outline-offset: 3px; }

@media (min-width: 36rem) {
  .entry-panel :deep(.entry-form--paired) { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .entry-panel :deep(.entry-form--paired > *) { grid-column: 1 / -1; min-width: 0; }
  .entry-panel :deep(.entry-form--paired > .entry-code) { grid-column: auto; }
}
@media (max-width: 35.99rem) {
  .entry-nav { width: calc(100% - 2rem); min-height: 3.75rem; }
  .entry-brand img { width: 2rem; height: 2rem; border-radius: .7rem; }
  .entry-main { align-items: start; padding: clamp(1.25rem, 4vh, 2rem) .875rem max(1.5rem, env(safe-area-inset-bottom)); }
  .entry-panel { border-radius: 1.375rem; }
  .entry-panel :deep(.auth-input) { font-size: 1rem; }
}
/* Give the native 300px verification strip room before falling back to compact.
   Keep the input, challenge and submit edges aligned; never scale or crop the iframe. */
@media (min-width: 302px) and (max-width: 35.99rem) {
  .entry-main:has(.turnstile-wrapper) {
    --entry-strip-room: 302px; /* Minimum strip width plus both panel borders. */
    padding-inline: clamp(0px, (100% - var(--entry-strip-room)) / 4, .875rem);
  }
  .entry-panel:has(.turnstile-wrapper) {
    padding-inline: clamp(0px, (100% - var(--entry-strip-room)) / 2, var(--entry-pad));
  }
}
@media (prefers-reduced-motion: reduce) {
  .entry-panel :deep(.btn), .entry-panel :deep(.auth-input) { transition: none; }
}
</style>
