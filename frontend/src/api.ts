// The only place the browser talks to the server.
//
// Every call uses a same-origin relative path with the session cookie, and the
// server's identity is the only identity the UI ever trusts.

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

/** An error the server described using its own envelope. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

/** SUPPORTED_EXTENSIONS mirrors the formats the server accepts. */
export const SUPPORTED_EXTENSIONS = ['.md', '.txt', '.pdf', '.docx'] as const

/** MAX_UPLOAD_BYTES mirrors the server's per-file ceiling. */
export const MAX_UPLOAD_BYTES = 5 * 1024 * 1024

interface ErrorEnvelope {
  error?: { code?: string; message?: string }
}

async function toApiError(response: Response): Promise<ApiError> {
  let code = 'unknown'
  let message = `请求失败（${response.status}）`

  try {
    const body = (await response.json()) as ErrorEnvelope
    if (body.error?.code) {
      code = body.error.code
    }
    if (body.error?.message) {
      message = body.error.message
    }
  } catch {
    // A non-JSON body leaves the generic message in place.
  }

  return new ApiError(response.status, code, message)
}

async function request(path: string, init: RequestInit = {}): Promise<Response> {
  const response = await fetch(path, {
    // The session is a cookie; nothing is kept in storage.
    credentials: 'same-origin',
    ...init,
  })

  if (!response.ok) {
    throw await toApiError(response)
  }

  return response
}

/**
 * getCurrentUser returns null when there is no valid session, and throws when
 * the failure was anything else — a network blip must not be mistaken for a
 * logged-out user.
 */
export async function getCurrentUser(): Promise<User | null> {
  try {
    const response = await request('/api/me')
    return (await response.json()) as User
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      return null
    }
    throw error
  }
}

export async function login(username: string, password: string): Promise<User> {
  const response = await request('/api/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })

  const body = (await response.json()) as { user: User }
  return body.user
}

export async function logout(): Promise<void> {
  await request('/api/logout', { method: 'POST' })
}

export async function listMaterials(): Promise<Material[]> {
  const response = await request('/api/materials')
  const body = (await response.json()) as { materials: Material[] | null }
  return body.materials ?? []
}

export async function getMaterial(id: string): Promise<MaterialDetail> {
  const response = await request(`/api/materials/${encodeURIComponent(id)}`)
  return (await response.json()) as MaterialDetail
}

export async function uploadMaterial(title: string, file: File): Promise<Material> {
  const form = new FormData()
  form.append('title', title)
  form.append('file', file)

  const response = await request('/api/materials', { method: 'POST', body: form })
  const body = (await response.json()) as { material: Material }
  return body.material
}

/**
 * downloadPath is a same-origin URL, so the browser sends the session cookie
 * and the server's Content-Disposition triggers the download. Originals are
 * never served statically.
 */
export function downloadPath(id: string): string {
  return `/api/materials/${encodeURIComponent(id)}/file`
}
