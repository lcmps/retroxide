# Frontend Code Review — Enhancement Opportunities

## Summary
The implementation is solid with good patterns and type safety. The following enhancements would improve code quality, maintainability, and consistency across the codebase.

---

## 1. Extract Shared `translate()` Helper ⭐ Priority: High

**Current State:** Every page (login, library, uploads, downloads) reimplements the same `translate()` function.

**Issue:**
```svelte
// login/+page.svelte
function translate(key: string, fallback: string, values?: Record<string, string | number>): string {
  const resolved = i18n.t(key, values);
  return resolved === key ? fallback : resolved;
}

// library/+page.svelte
function translate(key: string, fallback: string, values?: Record<string, string | number>): string {
  const resolved = i18n.t(key, values);
  return resolved === key ? fallback : resolved;
}
```

**Impact:** Code duplication, maintenance burden, inconsistent fallback behavior across pages.

**Recommendation:**
Move to `src/lib/i18n/helpers.ts`:
```typescript
export function useTranslate(i18n: typeof import('$lib').i18n) {
  return (key: string, fallback: string, values?: TranslationValues): string => {
    const resolved = i18n.t(key, values);
    return resolved === key ? fallback : resolved;
  };
}
```

Then use in components:
```svelte
<script>
  import { i18n } from '$lib';
  import { useTranslate } from '$lib/i18n/helpers';

  const t = useTranslate(i18n);
  // Now use: t('key', 'fallback')
</script>
```

---

## 2. Improve Fallback Detection Logic ⭐ Priority: High

**Current Issue:** The check `resolved === key` is fragile:
- If a translation value legitimately equals its key, this breaks
- If the backend returns the key as the value, this incorrectly treats it as untranslated
- Better to have an explicit flag from the i18n system

**Recommendation:**
Enhance the i18n store to track translation availability:

```typescript
// In store.svelte.ts
export const i18n = {
  // ... existing code
  hasTranslation(key: string): boolean {
    const activeState = getActiveState();
    const activeCatalog = activeState?.activeCatalog ?? EMBEDDED_ENGLISH_CATALOG;
    return key in activeCatalog;
  },

  t(key: string, values?: TranslationValues): string {
    // ... existing implementation
  }
};
```

Then use in components:
```svelte
{#if i18n.hasTranslation('login.messages.success')}
  <p>{i18n.t('login.messages.success', values)}</p>
{:else}
  <p>{fallback}</p>
{/if}
```

---

## 3. Centralize Error Message Handling ⭐ Priority: High

**Current State:**
- `uploads/+page.svelte` and `downloads/+page.svelte` use `getLocalizedErrorMessage()`
- `login/+page.svelte` and `library/+page.svelte` handle errors ad-hoc

**Recommendation:**
Create `src/lib/helpers/error.ts`:
```typescript
export function getErrorMessage(
  error: unknown,
  translationKey: string,
  fallback: string,
  translate: (key: string, fallback: string) => string
): string {
  if (error instanceof ApiClientError) {
    return getLocalizedErrorMessage(error, translate);
  }
  if (error instanceof Error) {
    return error.message;
  }
  return translate(translationKey, fallback);
}
```

Use consistently across all pages.

---

## 4. Add Type-Safe Translation Function ⭐ Priority: Medium

**Current Issue:** Translation keys are strings (no autocomplete, typos undetected):
```svelte
<h1>{translate('library.title', 'Library')}</h1>
// Typo: 'library.titlee' won't be caught at compile time
```

**Recommendation:**
Create a typed translation key type:
```typescript
// src/lib/i18n/keys.ts
export type TranslationKey =
  | 'app.nav.login'
  | 'app.nav.library'
  | 'login.title'
  | 'library.title'
  // ... (generated or manually maintained)

export function useTranslate(i18n: typeof import('$lib').i18n) {
  return (key: TranslationKey, fallback: string, values?: TranslationValues): string => {
    const resolved = i18n.t(key, values);
    return resolved === key ? fallback : resolved;
  };
}
```

**Note:** This requires maintaining a TypeScript union of all keys, which could be auto-generated from the backend catalogs.

---

## 5. Improve Promise Cleanup in i18n Store ⭐ Priority: Medium

**Current State (store.svelte.ts):**
```typescript
let englishCatalogPromise: Promise<TranslationCatalog> | null = null;
let latestLocaleRequestId = 0;
let pendingInitialization: Promise<void> | null = null;
```

**Issue:** `pendingInitialization` is declared but never used.

**Recommendation:**
Either use it or remove it. If intent is to deduplicate concurrent `initialize()` calls:
```typescript
async initialize(): Promise<void> {
  const activeState = getActiveState();
  if (!activeState) return;

  if (activeState.initialized) return;
  if (pendingInitialization) return pendingInitialization;

  pendingInitialization = (async () => {
    try {
      activeState.initializing = true;
      // ... initialization logic
    } finally {
      activeState.initializing = false;
      activeState.initialized = true;
      pendingInitialization = null;
    }
  })();

  return pendingInitialization;
}
```

---

## 6. Add Defensive Null/Undefined Checks ⭐ Priority: Medium

**Current Issues:**

In `downloads/+page.svelte`:
```typescript
function readQueue(): DownloadQueueItem[] {
  // Missing check for `parsed` not being an array before calling `.filter()`
  const parsed = JSON.parse(rawQueue) as DownloadQueueItem[];
  return Array.isArray(parsed) ? parsed.filter(...) : [];
}
```

**Better approach:**
```typescript
function readQueue(): DownloadQueueItem[] {
  if (!rawQueue) return [];
  try {
    const parsed = JSON.parse(rawQueue);
    if (!Array.isArray(parsed)) return [];

    return parsed.filter((item) => {
      return (
        typeof item?.romId === 'string' &&
        typeof item?.title === 'string' &&
        typeof item?.platform === 'string' &&
        ['ready', 'downloading', 'completed', 'failed'].includes(item?.status)
      );
    });
  } catch {
    return [];
  }
}
```

---

## 7. Use Svelte Callbacks for Async Operations ⭐ Priority: Medium

**Current State:**
```svelte
<button type="button" onclick={loadLibraries} disabled={loading}>
  {loading ? '...' : 'Refresh'}
</button>
```

Where `onclick={loadLibraries}` passes an event, but `loadLibraries` doesn't expect one.

**Better approach:**
```svelte
<button type="button" onclick={() => void loadLibraries()} disabled={loading}>
  {loading ? '...' : 'Refresh'}
</button>
```

Or create a wrapper:
```svelte
<script>
  async function handleRefresh(): Promise<void> {
    await loadLibraries();
  }
</script>

<button type="button" onclick={handleRefresh} disabled={loading}>
```

This is more explicit and avoids `void` suppression.

---

## 8. Improve onMount Organization ⭐ Priority: Low

**Current State:**
```svelte
onMount(async () => {
  await i18n.initialize();
  await loadLibraries();
});
```

**Issue:** Sequential execution (could parallelize if independent).

**Recommendation:**
```svelte
onMount(async () => {
  // Initialize i18n first (required before translation)
  await i18n.initialize();

  // Then load data independently
  await Promise.all([
    loadLibraries(),
    // other independent async operations
  ]);
});
```

Or extract to a separate function for clarity:
```svelte
<script>
  async function initializeAndLoad(): Promise<void> {
    await i18n.initialize();
    await loadLibraries();
  }
</script>

<svelte:script>
  onMount(initializeAndLoad);
</svelte:script>
```

---

## 9. Add Loading State Management Utility ⭐ Priority: Low

**Current State:** Every page reimplements loading/error/status message patterns:
```svelte
let loading = $state(false);
let errorMessage = $state('');
let statusMessage = $state('');

async function doSomething() {
  loading = true;
  errorMessage = '';
  statusMessage = '';
  try {
    // ...
  } catch (error) {
    errorMessage = getErrorMessage(error, 'key', 'fallback');
  } finally {
    loading = false;
  }
}
```

**Recommendation:**
Create `src/lib/helpers/async.ts`:
```typescript
export function createAsyncController() {
  let loading = $state(false);
  let error = $state<Error | null>(null);
  let status = $state('');

  async function run<T>(
    fn: () => Promise<T>,
    options?: { onError?: (e: Error) => string }
  ): Promise<T> {
    loading = true;
    error = null;
    status = '';

    try {
      return await fn();
    } catch (e) {
      error = e instanceof Error ? e : new Error(String(e));
      status = options?.onError?.(error) ?? error.message;
      throw error;
    } finally {
      loading = false;
    }
  }

  return { loading, error, status, run };
}
```

Use in components:
```svelte
<script>
  const { loading, error, status, run } = createAsyncController();

  async function handleSubmit() {
    await run(() => submitForm(), {
      onError: (e) => translate('common.errors.generic', 'Failed')
    });
  }
</script>
```

---

## 10. Improve Type Safety for Form Events ⭐ Priority: Low

**Current Issues:**

In `uploads/+page.svelte`:
```typescript
function updateSelectedFile(event: Event): void {
  const input = event.currentTarget as HTMLInputElement;
  // Unsafe cast; currentTarget could be anything
}
```

**Better approach:**
```typescript
function updateSelectedFile(event: Event): void {
  const input = event.currentTarget;
  if (!(input instanceof HTMLInputElement)) {
    console.error('Expected input element');
    return;
  }
  selectedFile = input.files?.[0] ?? null;
}
```

Or use Svelte's `bind:`:
```svelte
<input type="file" bind:files={selectedFiles} />

<script>
  let selectedFiles: FileList;
  $effect(() => {
    selectedFile = selectedFiles?.[0] ?? null;
  });
</script>
```

---

## 11. Add JSDoc Comments for Complex Functions ⭐ Priority: Low

**Current State:** Complex functions lack documentation:
```typescript
async function syncActiveCatalog(activeState: I18nState): Promise<void> {
  // No comment explaining the race-condition prevention logic
  const requestId = ++latestLocaleRequestId;
  // ...
}
```

**Recommendation:**
```typescript
/**
 * Synchronize the active catalog with the desired locale.
 * Uses request IDs to prevent race conditions from concurrent requests.
 * If fetch fails, falls back to English catalog.
 */
async function syncActiveCatalog(activeState: I18nState): Promise<void> {
  const requestId = ++latestLocaleRequestId;
  // ...
}
```

---

## 12. Performance: Memoize Translation Lookups (Optional) ⭐ Priority: Low

**Current State:** Every translation call does a key lookup in the catalog object.

**Consideration:** For high-frequency re-renders, could cache commonly-used translations:
```typescript
export function createCatalog(catalog: TranslationCatalog, englishCatalog: TranslationCatalog) {
  const cache = new Map<string, string>();

  return {
    t(key: string, values?: TranslationValues): string {
      if (cache.has(key)) {
        return interpolateTranslation(cache.get(key)!, values);
      }

      const resolved = resolveTranslation(catalog, englishCatalog, key, values);
      cache.set(key, resolved);
      return resolved;
    },

    clear() {
      cache.clear();
    }
  };
}
```

**Note:** Only needed if profiling shows translation lookups are a bottleneck.

---

## 13. Add Accessibility Enhancements ⭐ Priority: Low

**Opportunities:**
1. Use `role="status"` with `aria-live="polite"` for loading/error messages (partially done)
2. Add `aria-disabled` for disabled buttons
3. Add loading spinner announcement for screen readers

Example:
```svelte
{#if loading}
  <div role="status" aria-live="polite" aria-busy="true">
    <span class="sr-only">Loading...</span>
    <div class="spinner" aria-hidden="true"></div>
  </div>
{/if}
```

---

## 14. Logging/Debugging Support (Optional) ⭐ Priority: Low

**Current State:** No logging utilities for debugging issues.

**Recommendation:**
Create `src/lib/utils/logger.ts`:
```typescript
type LogLevel = 'debug' | 'info' | 'warn' | 'error';

const ENABLED_LEVELS: Set<LogLevel> = new Set(
  (import.meta.env.VITE_LOG_LEVEL ?? 'warn').split(',') as LogLevel[]
);

export const logger = {
  debug(msg: string, data?: unknown) { /* ... */ },
  info(msg: string, data?: unknown) { /* ... */ },
  warn(msg: string, data?: unknown) { /* ... */ },
  error(msg: string, data?: unknown) { /* ... */ }
};
```

Use in store and components for debugging i18n initialization, API calls, etc.

---

## Recommended Implementation Order

1. **High Priority (Quick Wins):**
   - Extract shared `translate()` helper → 15 min
   - Centralize error handling → 20 min
   - Fix unused `pendingInitialization` → 5 min

2. **Medium Priority (Quality):**
   - Improve fallback detection logic → 30 min
   - Add defensive null checks → 20 min
   - Improve async callbacks → 15 min

3. **Low Priority (Polish):**
   - Add JSDoc comments → 10 min
   - Improve type safety for forms → 15 min
   - Add accessibility enhancements → 20 min

**Estimated total implementation time: ~2-3 hours**

---

## Files to Create/Modify

**New files:**
- `src/lib/i18n/helpers.ts` (translate helper)
- `src/lib/helpers/error.ts` (error handling)
- `src/lib/helpers/async.ts` (async controller)
- `src/lib/utils/logger.ts` (optional logging)

**Files to update:**
- All route pages: `+page.svelte` (use new helpers)
- `src/lib/i18n/store.svelte.ts` (fix pending initialization)
- `src/lib/i18n/catalog.ts` (improve type safety)
- `src/lib/index.ts` (export new helpers)
