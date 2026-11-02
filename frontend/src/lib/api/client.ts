import { browser } from '$app/env';

import type {
  ApiError,
  DownloadLink,
  LoginRequest,
  MySQLConnection,
  RomDetail,
  RomListResponse,
  SessionResponse,
  SetupRequest,
  SetupStatus,
  UploadAcceptedResponse,
  UploadStatusResponse,
  UpdateRomRequest
} from './types';

import { parseTranslationCatalog, type Locale, type TranslationCatalog } from '#lib/i18n/catalog.js';
import { ApiClientError, parseApiErrorPayload } from './errors';
import {
  parseRomListResponse,
  parseSessionResponse,
  parseUploadAcceptedResponse
} from './schemas';

const API_BASE_URL = (import.meta.env.VITE_API_BASE_URL ?? '/api/v1').replace(/\/$/, '');
const SESSION_STORAGE_KEY = 'retroxide.session';
let refreshInFlight: Promise<boolean> | null = null;

type StoredSession = Pick<SessionResponse, 'access_token' | 'refresh_token'> & {
  expiresAt: number;
};

type ApiRequestOptions = RequestInit & {
  withAuth?: boolean;
};

function createApiUrl(path: string): string {
  return `${API_BASE_URL}${path}`;
}

function readStoredSession(): StoredSession | null {
  if (!browser) {
    return null;
  }

  const rawSession = sessionStorage.getItem(SESSION_STORAGE_KEY);
  if (!rawSession) {
    return null;
  }

  try {
    const parsed = JSON.parse(rawSession) as Partial<StoredSession>;
    if (
      typeof parsed.access_token !== 'string' ||
      parsed.access_token.length === 0 ||
      typeof parsed.refresh_token !== 'string' ||
      parsed.refresh_token.length === 0 ||
      typeof parsed.expiresAt !== 'number'
    ) {
      return null;
    }

    return {
      access_token: parsed.access_token,
      refresh_token: parsed.refresh_token,
      expiresAt: parsed.expiresAt
    };
  } catch {
    return null;
  }
}

export function persistStoredSession(session: SessionResponse): void {
  if (!browser) {
    return;
  }

  const storedSession: StoredSession = {
    access_token: session.access_token,
    refresh_token: session.refresh_token,
    expiresAt: Date.now() + session.expires_in * 1000
  };

  sessionStorage.setItem(SESSION_STORAGE_KEY, JSON.stringify(storedSession));
}

export function clearStoredSession(): void {
  if (!browser) {
    return;
  }

  sessionStorage.removeItem(SESSION_STORAGE_KEY);
}

export function hasStoredSession(): boolean {
  return readStoredSession() !== null;
}

function notifySessionExpired(): void {
  if (browser) {
    window.dispatchEvent(new Event('retroxide:session-expired'));
  }
}

async function performSessionRefresh(): Promise<boolean> {
  const session = readStoredSession();
  if (!session) {
    return false;
  }

  try {
    const response = await fetch(createApiUrl('/session/refresh'), {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: session.refresh_token })
    });

    if (!response.ok) {
      return false;
    }

    persistStoredSession(parseSessionResponse(await response.json()));
    return true;
  } catch {
    return false;
  }
}

function refreshStoredSession(): Promise<boolean> {
  if (!refreshInFlight) {
    const refresh = performSessionRefresh();
    refreshInFlight = refresh;
    void refresh.finally(() => {
      if (refreshInFlight === refresh) refreshInFlight = null;
    });
  }
  return refreshInFlight;
}

/** Refresh this long before expiry, so requests never go out with a token about to lapse. */
const REFRESH_AHEAD_MS = 60_000;

/**
 * Refreshes the session ahead of expiry instead of waiting for a 401. Besides saving a wasted
 * round trip, this keeps a large upload from being sent twice when the token lapses first.
 * A failed refresh is not fatal here: the request proceeds and normal 401 handling applies.
 */
async function ensureFreshSession(): Promise<void> {
  const session = readStoredSession();
  if (session && session.expiresAt - Date.now() < REFRESH_AHEAD_MS) {
    await refreshStoredSession();
  }
}

function createHeaders(init: ApiRequestOptions | undefined): Headers {
  const headers = new Headers(init?.headers);
  const shouldAttachAuth = init?.withAuth ?? false;

  if (!headers.has('Accept')) {
    headers.set('Accept', 'application/json');
  }

  if (shouldAttachAuth) {
    const session = readStoredSession();
    if (session) {
      headers.set('Authorization', `Bearer ${session.access_token}`);
    }
  }

  return headers;
}

async function requestResponse(path: string, init?: ApiRequestOptions): Promise<Response> {
  if (init?.withAuth) {
    await ensureFreshSession();
  }
  let response = await fetch(createApiUrl(path), {
    ...init,
    headers: createHeaders(init)
  });

  if (init?.withAuth && response.status === 401 && await refreshStoredSession()) {
    response = await fetch(createApiUrl(path), {
      ...init,
      headers: createHeaders(init)
    });
  }

  if (!response.ok) {
    if (init?.withAuth && response.status === 401) {
      clearStoredSession();
      notifySessionExpired();
    }
    throw await createResponseError(response);
  }
  return response;
}

async function requestJson<T>(path: string, init?: ApiRequestOptions): Promise<T> {
  return (await requestResponse(path, init).then((response) => response.json())) as T;
}

async function requestNoContent(path: string, init?: ApiRequestOptions): Promise<void> {
  await requestResponse(path, init);
}

async function createResponseError(response: Response): Promise<ApiClientError> {
  const responseType = response.headers.get('Content-Type') ?? '';

  if (responseType.includes('application/json')) {
    try {
      const payload = parseApiErrorPayload((await response.json()) as ApiError);
      if (payload) {
        return new ApiClientError({
          message: payload.message,
          code: payload.code,
          status: response.status,
          requestId: payload.request_id
        });
      }
    } catch {
      // Fall back to plain text handling below.
    }
  }

  const message = await response.text();
  return new ApiClientError({
    message: message || `API request failed with status ${response.status}`,
    status: response.status
  });
}

function parseJsonText<T>(payload: string, status: number): T {
  try {
    return JSON.parse(payload) as T;
  } catch {
    throw new Error(`API request failed with status ${status}`);
  }
}

function getXmlHttpRequestErrorMessage(request: XMLHttpRequest): string {
  const payload = request.responseText?.trim();
  if (!payload) {
    return `API request failed with status ${request.status}`;
  }

  try {
    const parsed = JSON.parse(payload) as { message?: unknown };
    if (typeof parsed.message === 'string' && parsed.message.length > 0) {
      return parsed.message;
    }
  } catch {
    // Fall back to returning the raw payload.
  }

  return payload;
}

function getXmlHttpRequestError(request: XMLHttpRequest): ApiClientError {
  const payload = request.responseText?.trim();
  if (payload) {
    try {
      const parsed = parseApiErrorPayload(JSON.parse(payload) as ApiError);
      if (parsed) {
        return new ApiClientError({
          message: parsed.message,
          code: parsed.code,
          status: request.status,
          requestId: parsed.request_id
        });
      }
    } catch {
      // Fall back to plain text handling below.
    }
  }

  return new ApiClientError({
    message: getXmlHttpRequestErrorMessage(request),
    status: request.status
  });
}

export async function checkHealth(): Promise<boolean> {
  const response = await fetch('/health', { method: 'GET' });
  if (!response.ok) {
    return false;
  }

  const body = await response.text();
  return body.trim().toLowerCase() === 'ok';
}

export function getSetupStatus(): Promise<SetupStatus> {
  return requestJson<SetupStatus>('/setup');
}

export function testMySQLConnection(connection: MySQLConnection): Promise<void> {
  return requestNoContent('/setup/mysql/test', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(connection)
  });
}

export function completeSetup(payload: SetupRequest): Promise<void> {
  return requestNoContent('/setup', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  });
}

export async function fetchCatalog(locale: Locale): Promise<TranslationCatalog> {
  return parseTranslationCatalog(await requestJson<unknown>(`/i18n/${encodeURIComponent(locale)}`));
}

/** Server-side ROM list filters: exact platform and case-insensitive search. */
export type RomListFilters = {
  platform?: string;
  q?: string;
};

export function listRoms(limit = 10, cursor?: string, filters: RomListFilters = {}): Promise<RomListResponse> {
  const params = new URLSearchParams({ limit: String(limit) });
  if (cursor) {
    params.set('cursor', cursor);
  }
  if (filters.platform) {
    params.set('platform', filters.platform);
  }
  if (filters.q) {
    params.set('q', filters.q);
  }

  return requestJson<unknown>(`/rom?${params.toString()}`, { withAuth: true }).then(parseRomListResponse);
}

export function createSession(payload: LoginRequest): Promise<SessionResponse> {
  return requestJson<unknown>('/session', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(payload)
  }).then(parseSessionResponse);
}

export async function logoutSession(): Promise<void> {
  try {
    await requestNoContent('/session', { method: 'DELETE', withAuth: true });
  } finally {
    clearStoredSession();
  }
}

export function getRom(romId: string): Promise<RomDetail> {
  return requestJson<RomDetail>(`/rom/${romId}`, { withAuth: true });
}

export function deleteRom(romId: string): Promise<void> {
  return requestNoContent(`/rom/${romId}`, {
    method: 'DELETE',
    withAuth: true
  });
}

export function updateRom(romId: string, payload: UpdateRomRequest): Promise<RomDetail> {
  return requestJson<RomDetail>(`/rom/${encodeURIComponent(romId)}`, {
    method: 'PATCH',
    withAuth: true,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  });
}

export function uploadRomCover(romId: string, file: File): Promise<RomDetail> {
  const formData = new FormData();
  formData.append('cover', file, file.name);
  return requestJson<RomDetail>(`/rom/${encodeURIComponent(romId)}/cover`, {
    method: 'POST',
    withAuth: true,
    body: formData
  });
}

/**
 * Fetches a ROM's cover. Passing the summary's cover_art_url keeps its content version, so the
 * browser can serve repeat requests from its HTTP cache.
 */
export async function fetchRomCover(romId: string, coverArtUrl?: string | null): Promise<Blob> {
  const version = coverArtUrl ? new URL(coverArtUrl, 'http://localhost').search : '';
  const response = await requestResponse(`/rom/${encodeURIComponent(romId)}/cover${version}`, { withAuth: true });
  return response.blob();
}

export function scrapeRom(romId: string): Promise<RomDetail> {
  return requestJson<RomDetail>(`/rom/${encodeURIComponent(romId)}/scrape`, {
    method: 'POST',
    withAuth: true
  });
}

export function uploadRom(
  file: File,
  onProgress?: (loadedBytes: number, totalBytes: number) => void
): Promise<UploadAcceptedResponse> {
  if (!browser) {
    return Promise.reject(new Error('Upload is only available in the browser.'));
  }

  if (!readStoredSession()) {
    return Promise.reject(new Error('Missing stored session token.'));
  }

  const sendUpload = (): Promise<XMLHttpRequest> => new Promise((resolve, reject) => {
    const request = new XMLHttpRequest();
    request.open('POST', createApiUrl('/upload'));
    request.responseType = 'text';
    request.setRequestHeader('Accept', 'application/json');
    const session = readStoredSession();
    if (!session) {
      reject(new Error('Missing stored session token.'));
      return;
    }
    request.setRequestHeader('Authorization', `Bearer ${session.access_token}`);

    request.upload.addEventListener('progress', (event) => {
      if (!onProgress || !event.lengthComputable) {
        return;
      }

      onProgress(event.loaded, event.total);
    });

    request.addEventListener('load', () => {
      resolve(request);
    });

    request.addEventListener('error', () => {
      reject(new ApiClientError({ message: 'Network request failed' }));
    });

    const formData = new FormData();
    formData.append('file', file, file.name);
    request.send(formData);
  });

  return (async () => {
  await ensureFreshSession();
  let request = await sendUpload();
  if (request.status === 401 && await refreshStoredSession()) request = await sendUpload();
  if (request.status < 200 || request.status >= 300) {
    if (request.status === 401) {
      clearStoredSession();
      notifySessionExpired();
    }
    throw getXmlHttpRequestError(request);
  }
  return parseUploadAcceptedResponse(parseJsonText<unknown>(request.responseText, request.status));
  })();
}

export function uploadStatus(uploadId: string): Promise<UploadStatusResponse> {
  return requestJson<UploadStatusResponse>(`/upload/${encodeURIComponent(uploadId)}`, {
    withAuth: true
  });
}

/**
 * Requests a signed, short-lived download link. Opening it lets the browser stream the file to
 * disk with its own download UI, instead of buffering the whole ROM in page memory.
 */
export async function createDownloadLink(romId: string): Promise<DownloadLink> {
  const link = await requestJson<{ url: string; filename: string; expires_in: number }>(
    `/rom/${encodeURIComponent(romId)}/download-link`,
    { method: 'POST', withAuth: true }
  );
  return { url: resolveApiLink(link.url), filename: link.filename, expiresIn: link.expires_in };
}

/** Starts a native browser download of a ROM and returns the link it opened. */
export async function downloadRom(romId: string): Promise<DownloadLink> {
  const link = await createDownloadLink(romId);
  const anchor = document.createElement('a');
  anchor.href = link.url;
  anchor.download = link.filename;
  anchor.rel = 'noopener';
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  return link;
}

/** Resolves a server-relative link against the API origin, which may differ from the page's. */
function resolveApiLink(path: string): string {
  const pageURL = globalThis.location?.href ?? 'http://localhost';
  return new URL(path, new URL(API_BASE_URL, pageURL)).href;
}
