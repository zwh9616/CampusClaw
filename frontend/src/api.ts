export type Role = 'teacher' | 'student'

export interface User {
  id: string
  username: string
  role: Role
  class_id: string
  class_name: string
}

export interface Material {
  id: string
  class_id: string
  uploaded_by: string
  title: string
  original_filename: string
  content_type: string
  created_at: string
}

export interface MaterialDetail {
  material: Material
  content: string
}

export class ApiError extends Error {
  constructor(readonly status: number, readonly code: string, message: string) {
    super(message)
    this.name = 'ApiError'
  }
}

export const SUPPORTED_EXTENSIONS = ['.md', '.txt', '.pdf', '.docx'] as const
export const MAX_UPLOAD_BYTES = 5 * 1024 * 1024

interface ErrorEnvelope {
  error?: { code?: string; message?: string }
}

interface AuthResponse {
  user: User
  token: string
  expires_at: string
}

// The opaque access token is the only credential there is — there is no cookie
// and no refresh step. It carries no identity: the server re-reads role and
// class from the database on every request and never trusts a client claim, so
// holding this value grants nothing on its own.
const TOKEN_STORAGE_KEY = 'campusclaw.access_token'

function storedToken(): string | null {
  return localStorage.getItem(TOKEN_STORAGE_KEY)
}

function storeToken(token: string): void {
  localStorage.setItem(TOKEN_STORAGE_KEY, token)
}

function forgetToken(): void {
  localStorage.removeItem(TOKEN_STORAGE_KEY)
}

async function toApiError(response: Response): Promise<ApiError> {
  let code = 'unknown'
  let message = '请求失败（' + response.status + '）'
  try {
    const body = (await response.json()) as ErrorEnvelope
    if (body.error?.code) code = body.error.code
    if (body.error?.message) message = body.error.message
  } catch {
    // Keep the generic message for a non-JSON response.
  }
  return new ApiError(response.status, code, message)
}

async function request(path: string, init: RequestInit = {}): Promise<Response> {
  const headers = new Headers(init.headers)
  const token = storedToken()
  if (token !== null) headers.set('Authorization', 'Bearer ' + token)

  const response = await fetch(path, { ...init, headers })
  if (!response.ok) {
    // There is no refresh credential to fall back on, so a rejected token is
    // simply gone: drop it and let the caller show the login page.
    if (response.status === 401) forgetToken()
    throw await toApiError(response)
  }
  return response
}

// restoreSession returns the signed-in user, or null when there is no usable
// stored token. A network or 5xx failure is thrown, so the caller can offer a
// retry rather than silently signing the user out.
export async function restoreSession(): Promise<User | null> {
  if (storedToken() === null) return null

  try {
    const response = await request('/api/me')
    return (await response.json()) as User
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) return null
    throw error
  }
}

export async function getCurrentUser(): Promise<User | null> {
  return restoreSession()
}

export async function login(username: string, password: string): Promise<User> {
  const response = await fetch('/api/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
  if (!response.ok) throw await toApiError(response)
  const body = (await response.json()) as AuthResponse
  storeToken(body.token)
  return body.user
}

export async function logout(): Promise<void> {
  try {
    await request('/api/logout', { method: 'POST' })
    forgetToken()
  } catch (error) {
    // A 401 already dropped the token inside request(); anything else (network,
    // 5xx) still throws, so the caller must not report a successful sign-out.
    if (error instanceof ApiError && error.status === 401) forgetToken()
    throw error
  }
}

export async function listMaterials(): Promise<Material[]> {
  const response = await request('/api/materials')
  const body = (await response.json()) as { materials: Material[] | null }
  return body.materials ?? []
}

export async function getMaterial(id: string): Promise<MaterialDetail> {
  const response = await request('/api/materials/' + encodeURIComponent(id))
  return (await response.json()) as MaterialDetail
}

export async function uploadMaterial(
  title: string,
  file: File,
  strategy: ChunkStrategy = DEFAULT_CHUNK_STRATEGY,
): Promise<Material> {
  const form = new FormData()
  form.append('title', title)
  form.append('file', file)
  appendChunkFields(form, strategy)
  const response = await request('/api/materials', { method: 'POST', body: form })
  const body = (await response.json()) as { material: Material }
  return body.material
}

/** SearchMode is one of the three retrieval strategies the API offers. */
export type SearchMode = 'keyword' | 'vector' | 'hybrid'

export const SEARCH_MODES: readonly SearchMode[] = ['hybrid', 'keyword', 'vector']

export const SEARCH_MODE_LABELS: Record<SearchMode, string> = {
  hybrid: '混合',
  keyword: '关键字',
  vector: '语义',
}

/**
 * SearchHit is one retrieved slice. The excerpt is plain text taken from the
 * relational store; no vector component is ever part of it.
 */
export interface SearchHit {
  material_id: string
  title: string
  chunk_id: string
  chunk_index: number
  start_offset: number
  end_offset: number
  offset_basis: 'extracted' | 'normalized'
  excerpt: string
  source: string
  keyword_score: number | null
  keyword_rank: number | null
  vector_score: number | null
  vector_rank: number | null
  rrf_score: number | null
}

export interface SearchResult {
  query_vector_generated: boolean
  message?: string
  hits: SearchHit[] | null
}

export interface AnswerResult {
  answer: string
  citations: SearchHit[] | null
}

export type IndexStatus = 'none' | 'pending' | 'ready' | 'failed'

export interface IndexState {
  status: IndexStatus
  generation: number
  strategy: string
  chunk_max_chars: number
  chunk_overlap_percent: number
  chunk_separator: string
  remove_urls_emails: boolean
  fold_whitespace: boolean
  failure_code: string
  updated_at?: string
}

/** ChunkStrategy is the optional split configuration an upload may carry. */
export interface ChunkStrategy {
  strategy: 'auto' | 'custom' | 'hierarchy'
  maxChars: string
  overlapPercent: string
  separator: string
  removeUrlsEmails: boolean
  foldWhitespace: boolean
}

export const CHUNK_STRATEGIES = ['auto', 'custom', 'hierarchy'] as const

export const CHUNK_STRATEGY_LABELS: Record<ChunkStrategy['strategy'], string> = {
  auto: '自动（800 字，重叠 80 字）',
  custom: '自定义',
  hierarchy: '按 Markdown 标题分章',
}

export const CHUNK_SEPARATORS = ['newline', 'blank_line', 'sentence'] as const

export const CHUNK_SEPARATOR_LABELS: Record<string, string> = {
  newline: '换行',
  blank_line: '空行',
  sentence: '句号',
}

export const DEFAULT_CHUNK_STRATEGY: ChunkStrategy = {
  strategy: 'auto',
  maxChars: '800',
  overlapPercent: '10',
  separator: 'newline',
  removeUrlsEmails: false,
  foldWhitespace: false,
}

/**
 * appendChunkFields adds the split parameters a strategy actually uses.
 *
 * The server ignores the fields a strategy does not take, so sending them would
 * be harmless — but sending only what applies keeps a custom configuration from
 * looking as if it applied to an automatic split.
 */
function appendChunkFields(form: FormData, strategy: ChunkStrategy): void {
  form.append('chunk_strategy', strategy.strategy)

  if (strategy.strategy !== 'custom') return

  form.append('chunk_max_chars', strategy.maxChars)
  form.append('chunk_overlap_percent', strategy.overlapPercent)
  form.append('chunk_separator', strategy.separator)
  form.append('remove_urls_emails', strategy.removeUrlsEmails ? 'true' : 'false')
  form.append('fold_whitespace', strategy.foldWhitespace ? 'true' : 'false')
}

/** search asks the API for slices of this class's material. */
export async function search(query: string, mode: SearchMode): Promise<SearchResult> {
  const response = await request('/api/search', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ query, mode }),
  })
  const body = (await response.json()) as SearchResult
  return { ...body, hits: body.hits ?? [] }
}

/** ask gets a short answer, generated only from this class's material. */
export async function ask(question: string): Promise<AnswerResult> {
  const response = await request('/api/ask', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ question }),
  })
  const body = (await response.json()) as AnswerResult
  return { ...body, citations: body.citations ?? [] }
}

/** getIndexState reports how one material is indexed. Teachers only. */
export async function getIndexState(materialId: string): Promise<IndexState> {
  const response = await request('/api/materials/' + encodeURIComponent(materialId) + '/index')
  const body = (await response.json()) as { index: IndexState }
  return body.index
}

/** reindexMaterial rebuilds one material's index. Teachers only. */
export async function reindexMaterial(
  materialId: string,
  strategy: ChunkStrategy,
): Promise<IndexState> {
  const payload: Record<string, unknown> = { chunk_strategy: strategy.strategy }

  if (strategy.strategy === 'custom') {
    payload.chunk_max_chars = Number(strategy.maxChars)
    payload.chunk_overlap_percent = Number(strategy.overlapPercent)
    payload.chunk_separator = strategy.separator
    payload.remove_urls_emails = strategy.removeUrlsEmails
    payload.fold_whitespace = strategy.foldWhitespace
  }

  const response = await request(
    '/api/materials/' + encodeURIComponent(materialId) + '/reindex',
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    },
  )
  const body = (await response.json()) as { index: IndexState }
  return body.index
}

function safeFilename(header: string | null, fallback: string): string {
  const utf8 = header?.match(/(?:^|;)\s*filename\*=UTF-8''([^;]+)/i)
  const quoted = header?.match(/(?:^|;)\s*filename="((?:\\.|[^"\\])*)"/i)
  const plain = header?.match(/(?:^|;)\s*filename=([^;\s]+)/i)
  let name = fallback
  try {
    if (utf8) name = decodeURIComponent(utf8[1])
    else if (quoted) name = quoted[1].replace(/\\(.)/g, '$1')
    else if (plain) name = plain[1]
  } catch {
    name = fallback
  }
  return name.replace(/[\/\\\u0000-\u001f\u007f]/g, '_').trim() || 'material'
}

export async function downloadMaterial(id: string, fallbackFilename: string): Promise<void> {
  const response = await request('/api/materials/' + encodeURIComponent(id) + '/file')
  const blob = await response.blob()
  const url = URL.createObjectURL(blob)
  try {
    const link = document.createElement('a')
    link.href = url
    link.download = safeFilename(response.headers.get('Content-Disposition'), fallbackFilename)
    document.body.append(link)
    link.click()
    link.remove()
  } finally {
    window.setTimeout(() => URL.revokeObjectURL(url), 0)
  }
}
