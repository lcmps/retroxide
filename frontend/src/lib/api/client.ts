import { browser } from '$app/env';

import type {
  Bios,
  BiosLabels,
  BiosUploadOptions,
  ApiError,
  DownloadLink,
  Appearance,
  CreateUserRequest,
  DeleteUserResponse,
  ManagedUser,
  UpdateUserRequest,
  Platform,
  Profile,
  SettingsResponse,
  UpdateProfileRequest,
  UpdateProfileResponse,
  LoginRequest,
  MySQLConnection,
  RomDetail,
  RomListResponse,
  SessionResponse,
  SetupRequest,
  SetupStatus,
  UploadAcceptedResponse,
  UploadStatusResponse,
  UpdateRomRequest,
  DownloadFormat,
  UploadOptions
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
/** A library order: by ID (the default, when absent), or a field in either direction. */
export type RomSortField = 'title' | 'release' | 'publisher' | 'genre';
export type RomSortOrder = 'asc' | 'desc';

export type RomListFilters = {
  platform?: string;
  q?: string;
  sort?: RomSortField;
  order?: RomSortOrder;
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
  if (filters.sort) {
    params.set('sort', filters.sort);
    params.set('order', filters.order ?? 'asc');
  }

  return requestJson<unknown>(`/rom?${params.toString()}`, { withAuth: true }).then(parseRomListResponse);
}

export function getProfile(): Promise<Profile> {
  return requestJson<Profile>('/me', { withAuth: true });
}

/** Updates the profile; after a password change, keeps this tab signed in with the new session. */
export async function updateProfile(payload: UpdateProfileRequest): Promise<Profile> {
  const response = await requestJson<UpdateProfileResponse>('/me', {
    method: 'PATCH',
    withAuth: true,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  });
  if (response.session) {
    persistStoredSession(response.session);
  }
  return response.profile;
}

/** The presentation settings, such as the interface font; available without signing in. */
export function getAppearance(): Promise<Appearance> {
  return requestJson<Appearance>('/appearance');
}

/** Lists every account (administrators only). */
export async function listUsers(): Promise<ManagedUser[]> {
  const response = await requestJson<{ item: ManagedUser[] }>('/users', { withAuth: true });
  return response.item;
}

export function createUser(payload: CreateUserRequest): Promise<ManagedUser> {
  return requestJson<ManagedUser>('/users', {
    method: 'POST',
    withAuth: true,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  });
}

export function updateUser(userId: string, payload: UpdateUserRequest): Promise<ManagedUser> {
  return requestJson<ManagedUser>(`/users/${encodeURIComponent(userId)}`, {
    method: 'PATCH',
    withAuth: true,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  });
}

/** Deletes an account; its ROMs, libraries, and uploads move to the signed-in administrator. */
export function deleteUser(userId: string): Promise<DeleteUserResponse> {
  return requestJson<DeleteUserResponse>(`/users/${encodeURIComponent(userId)}`, { method: 'DELETE', withAuth: true });
}

/** Signs a user out on every device. */
export function endUserSessions(userId: string): Promise<void> {
  return requestNoContent(`/users/${encodeURIComponent(userId)}/sessions`, { method: 'DELETE', withAuth: true });
}

export function getSettings(): Promise<SettingsResponse> {
  return requestJson<SettingsResponse>('/settings', { withAuth: true });
}

/** Saves settings (administrators only); an empty value restores the default. */
export function updateSettings(values: Record<string, string>): Promise<SettingsResponse> {
  return requestJson<SettingsResponse>('/settings', {
    method: 'PATCH',
    withAuth: true,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ values })
  });
}

/** Lists every platform Retroxide knows, for choosing one when editing a ROM. */
export async function listPlatformCatalog(): Promise<Platform[]> {
  const response = await requestJson<{ item: Platform[] }>('/platforms/catalog', { withAuth: true });
  return response.item;
}

/** Lists the platforms of the ROMs the current user can see, ordered for display. */
export async function listPlatforms(): Promise<Platform[]> {
  const response = await requestJson<{ item: Platform[] }>('/platforms', { withAuth: true });
  return response.item;
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
 * Fetches a ROM's cover, or with thumbnail its small copy for grids. Passing the summary's
 * cover_art_url (or cover_thumb_url) keeps its content version, so the browser can serve repeat
 * requests from its HTTP cache.
 */
export async function fetchRomCover(romId: string, coverUrl?: string | null, thumbnail = false): Promise<Blob> {
  const version = coverUrl ? new URL(coverUrl, 'http://localhost').search : '';
  const resource = thumbnail ? 'cover/thumb' : 'cover';
  const response = await requestResponse(`/rom/${encodeURIComponent(romId)}/${resource}${version}`, { withAuth: true });
  return response.blob();
}

export function scrapeRom(romId: string): Promise<RomDetail> {
  return requestJson<RomDetail>(`/rom/${encodeURIComponent(romId)}/scrape`, {
    method: 'POST',
    withAuth: true
  });
}

/** Aborts request when signal does, rejecting with the signal's reason; reports if it already has. */
function abortWith(request: XMLHttpRequest, signal: AbortSignal | undefined, reject: (reason: unknown) => void): boolean {
  if (!signal) return false;
  if (signal.aborted) {
    reject(signal.reason);
    return true;
  }
  const abort = () => request.abort();
  signal.addEventListener('abort', abort, { once: true });
  request.addEventListener('abort', () => reject(signal.reason));
  request.addEventListener('loadend', () => signal.removeEventListener('abort', abort));
  return false;
}

/** The path a single-request upload goes to; BIOS options ride in the query string. */
export function uploadPath(bios?: BiosUploadOptions): string {
  if (!bios) return '/upload';
  return `/upload?${new URLSearchParams({ bios: 'true', platform: bios.platform })}`;
}

export function uploadRom(
  file: File,
  onProgress?: (loadedBytes: number, totalBytes: number) => void,
  signal?: AbortSignal,
  bios?: BiosUploadOptions
): Promise<UploadAcceptedResponse> {
  if (!browser) {
    return Promise.reject(new Error('Upload is only available in the browser.'));
  }

  if (!readStoredSession()) {
    return Promise.reject(new Error('Missing stored session token.'));
  }

  const sendUpload = (): Promise<XMLHttpRequest> => new Promise((resolve, reject) => {
    const request = new XMLHttpRequest();
    if (abortWith(request, signal, reject)) return;
    request.open('POST', createApiUrl(uploadPath(bios)));
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

/** An upload sent in parts, which resumes after a dropped connection. */
export type UploadSession = {
  session_id: string;
  size: number;
  /** Bytes received so far: where the next part starts. */
  received: number;
  max_chunk_size: number;
  /** Set once every byte has arrived and the upload was accepted. */
  upload?: UploadAcceptedResponse;
  /** Set once every byte of a disc's file has arrived. */
  part_id?: string;
};

/** Starts an upload sent in parts; with part, the file is one of a disc's files. */
/** The body that starts an upload sent in parts. */
export function sessionRequestBody(file: File, part: boolean, bios?: BiosUploadOptions): Record<string, unknown> {
  const body: Record<string, unknown> = { filename: file.name, size: file.size };
  if (file.type) body.mime_type = file.type;
  if (part) body.part = true;
  if (bios) Object.assign(body, { bios: true, platform: bios.platform });
  return body;
}

export function createUploadSession(file: File, part = false, bios?: BiosUploadOptions): Promise<UploadSession> {
  return requestJson<UploadSession>('/upload/session', {
    method: 'POST',
    withAuth: true,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(sessionRequestBody(file, part, bios))
  });
}

/** Lists a platform's BIOS files, which every user shares. */
export async function listBios(platform: string): Promise<Bios[]> {
  const response = await requestJson<{ items: Bios[] }>(`/bios?${new URLSearchParams({ platform })}`, { withAuth: true });
  return response.items;
}

/** Sets the labels of a BIOS file the known list did not recognize. */
export function labelBios(biosId: string, labels: BiosLabels): Promise<Bios> {
  return requestJson<Bios>(`/bios/${encodeURIComponent(biosId)}`, {
    method: 'PATCH',
    withAuth: true,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(labels)
  });
}

export function deleteBios(biosId: string): Promise<void> {
  return requestNoContent(`/bios/${encodeURIComponent(biosId)}`, { method: 'DELETE', withAuth: true });
}

/** Starts a native browser download of a BIOS file, named as emulators expect it. */
export async function downloadBios(biosId: string): Promise<DownloadLink> {
  const link = await requestJson<{ url: string; filename: string; expires_in: number }>(
    `/bios/${encodeURIComponent(biosId)}/download-link`,
    { method: 'POST', withAuth: true }
  );
  return startDownload({ url: resolveApiLink(link.url), filename: link.filename, expiresIn: link.expires_in });
}

/** Stores a disc from its sheet and the uploaded parts holding its track files. */
export function assembleDisc(sheetName: string, sheet: string, parts: string[], compress = false): Promise<UploadAcceptedResponse> {
  return requestJson<unknown>('/upload/disc', {
    method: 'POST',
    withAuth: true,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sheet_name: sheetName, sheet, parts, compress: compress || undefined })
  }).then(parseUploadAcceptedResponse);
}

/** What the upload page can offer, such as compressing discs to CHD. */
export function getUploadOptions(): Promise<UploadOptions> {
  return requestJson<UploadOptions>('/upload/options', { withAuth: true });
}

/** Asks for a disc kept as tracks to be compressed to CHD in the background. */
export function requestCompression(romId: string, disc?: number): Promise<RomDetail> {
  const query = disc ? `?disc=${disc}` : '';
  return requestJson<RomDetail>(`/rom/${encodeURIComponent(romId)}/compress${query}`, { method: 'POST', withAuth: true });
}

/** Moves every disc of the source ROM onto this one and deletes the source. */
export function mergeRom(romId: string, sourceRomId: string): Promise<RomDetail> {
  return requestJson<RomDetail>(`/rom/${encodeURIComponent(romId)}/merge`, {
    method: 'POST',
    withAuth: true,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ source_rom_id: sourceRomId })
  });
}

/** Turns one disc into a ROM of its own; resolves with that new ROM. */
export function splitDisc(romId: string, number: number): Promise<RomDetail> {
  return requestJson<RomDetail>(`/rom/${encodeURIComponent(romId)}/discs/${number}/split`, { method: 'POST', withAuth: true });
}

/** Numbers the discs in the order of the file IDs, which must name every disc. */
export function reorderDiscs(romId: string, romFileIds: string[]): Promise<RomDetail> {
  return requestJson<RomDetail>(`/rom/${encodeURIComponent(romId)}/discs/order`, {
    method: 'PUT',
    withAuth: true,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ rom_file_ids: romFileIds })
  });
}

/** Permanently removes a disc and its files; the server refuses (409) if that number no longer is that file. */
export function removeDisc(romId: string, number: number, romFileId: string): Promise<RomDetail> {
  return requestJson<RomDetail>(`/rom/${encodeURIComponent(romId)}/discs/${number}?rom_file_id=${encodeURIComponent(romFileId)}`, { method: 'DELETE', withAuth: true });
}

export function getUploadSession(sessionId: string): Promise<UploadSession> {
  return requestJson<UploadSession>(`/upload/session/${encodeURIComponent(sessionId)}`, { withAuth: true });
}

/** Reports the file's SHA-256; a ROM the library already has ends the session with a 409. */
export function checkUploadSessionDuplicate(sessionId: string, sha256: string): Promise<void> {
  return requestNoContent(`/upload/session/${encodeURIComponent(sessionId)}/check`, {
    method: 'POST',
    withAuth: true,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sha256 })
  });
}

export function cancelUploadSession(sessionId: string): Promise<void> {
  return requestNoContent(`/upload/session/${encodeURIComponent(sessionId)}`, { method: 'DELETE', withAuth: true });
}

/**
 * Sends one part of a session upload, starting at offset. Progress reports the part's bytes.
 * Network failures reject with an ApiClientError without a status.
 */
export function sendUploadPart(
  sessionId: string,
  offset: number,
  part: Blob,
  onProgress?: (loadedBytes: number) => void,
  signal?: AbortSignal
): Promise<UploadSession> {
  const send = (): Promise<XMLHttpRequest> => new Promise((resolve, reject) => {
    const session = readStoredSession();
    if (!session) {
      reject(new Error('Missing stored session token.'));
      return;
    }
    const request = new XMLHttpRequest();
    if (abortWith(request, signal, reject)) return;
    request.open('PATCH', createApiUrl(`/upload/session/${encodeURIComponent(sessionId)}`));
    request.responseType = 'text';
    request.setRequestHeader('Accept', 'application/json');
    request.setRequestHeader('Authorization', `Bearer ${session.access_token}`);
    request.setRequestHeader('Content-Type', 'application/offset+octet-stream');
    request.setRequestHeader('Upload-Offset', String(offset));
    request.upload.addEventListener('progress', (event) => onProgress?.(event.loaded));
    request.addEventListener('load', () => resolve(request));
    request.addEventListener('error', () => reject(new ApiClientError({ message: 'Network request failed' })));
    request.addEventListener('timeout', () => reject(new ApiClientError({ message: 'Network request timed out' })));
    request.send(part);
  });

  return (async () => {
    await ensureFreshSession();
    let request = await send();
    if (request.status === 401 && await refreshStoredSession()) request = await send();
    if (request.status < 200 || request.status >= 300) {
      if (request.status === 401) {
        clearStoredSession();
        notifySessionExpired();
      }
      throw getXmlHttpRequestError(request);
    }
    return parseJsonText<UploadSession>(request.responseText, request.status);
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
export async function createDownloadLink(romId: string, format?: DownloadFormat, disc?: number): Promise<DownloadLink> {
  const params = new URLSearchParams();
  if (format) params.set('format', format);
  if (disc) params.set('disc', String(disc));
  const query = params.size ? `?${params}` : '';
  const link = await requestJson<{ url: string; filename: string; expires_in: number }>(
    `/rom/${encodeURIComponent(romId)}/download-link${query}`,
    { method: 'POST', withAuth: true }
  );
  return { url: resolveApiLink(link.url), filename: link.filename, expiresIn: link.expires_in };
}

/**
 * Starts a native browser download of a ROM, in one of the formats its details list (its
 * first when omitted), and returns the link it opened.
 */
export async function downloadRom(romId: string, format?: DownloadFormat, disc?: number): Promise<DownloadLink> {
  return startDownload(await createDownloadLink(romId, format, disc));
}

/** Opens a signed download link so the browser streams the file to disk itself. */
function startDownload(link: DownloadLink): DownloadLink {
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
