import { useCallback, useEffect, useState, type FormEvent } from 'react'
import * as api from '../api'
import type { ChunkStrategy, Material, MaterialDetail, User } from '../api'
import Brand from '../components/Brand'
import ChunkStrategyFields from '../components/ChunkStrategyFields'
import IndexPanel from '../components/IndexPanel'
import RetrievalPanel from '../components/RetrievalPanel'

interface Props {
  user: User
  onSignOut: () => void
  /** Called when the server reports the session is gone. */
  onUnauthorized: () => void
}

/** ACCEPT is the file picker hint; the server enforces the same list. */
const ACCEPT = api.SUPPORTED_EXTENSIONS.join(',')

export default function MaterialsPage({ user, onSignOut, onUnauthorized }: Props) {
  const [materials, setMaterials] = useState<Material[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [detail, setDetail] = useState<MaterialDetail | null>(null)

  /**
   * run funnels every server call through one place, so a 401 always returns to
   * the login page and a transport failure is never mistaken for one.
   */
  const run = useCallback(
    async <T,>(operation: () => Promise<T>): Promise<T | undefined> => {
      try {
        return await operation()
      } catch (failure) {
        if (failure instanceof api.ApiError) {
          if (failure.status === 401) {
            onUnauthorized()
            return undefined
          }
          setError(failure.message)
          return undefined
        }
        setError('网络错误，请重试。')
        return undefined
      }
    },
    [onUnauthorized],
  )

  const refresh = useCallback(async () => {
    setLoading(true)
    const loaded = await run(() => api.listMaterials())
    if (loaded !== undefined) {
      setMaterials(loaded)
      setError(null)
    }
    setLoading(false)
  }, [run])

  useEffect(() => {
    void refresh()
  }, [refresh])

  async function handleSignOut() {
    try {
      await api.logout()
      onSignOut()
    } catch (failure) {
      if (failure instanceof api.ApiError && failure.status === 401) {
        onSignOut()
      } else {
        setError(failure instanceof api.ApiError ? failure.message : '网络错误，请重试。')
      }
    }
  }

  /**
   * openMaterial loads one material's detail.
   *
   * A search hit names its material by id rather than by row, so the list and
   * the retrieval panel reach the detail the same way; the class check stays on
   * the server in both cases.
   */
  const openMaterial = useCallback(
    async (materialId: string) => {
      setDetail(null)
      setNotice(null)

      const loaded = await run(() => api.getMaterial(materialId))
      if (loaded !== undefined) {
        setDetail(loaded)
      }
    },
    [run],
  )

  return (
    <main className="app materials-page">
      <header className="bar site-header">
        <Brand />
        <div>
          <strong>{user.username}</strong>
          <span className="muted">
            {' '}
            · {user.role === 'teacher' ? '教师' : '学生'} · {user.class_name}（班级 ID {user.class_id}）
          </span>
        </div>
        <button type="button" className="secondary" onClick={handleSignOut}>
          登出
        </button>
      </header>

      <div className="page-intro">
        <p className="eyebrow">CLASS MATERIALS</p>
        <h1>本班材料</h1>
        <p className="muted">整理与分享课程资料，让班级学习更有条理。</p>
      </div>

      {error !== null && (
        <div>
          <p className="error" role="alert">{error}</p>
          <button type="button" className="secondary" onClick={() => void refresh()}>重试</button>
        </div>
      )}
      {notice !== null && <p className="notice" role="status">{notice}</p>}

      <RetrievalPanel
        onUnauthorized={onUnauthorized}
        onOpenMaterial={(materialId) => void openMaterial(materialId)}
      />

      <div className="dashboard-grid">
        {user.role === 'teacher' && (
          <UploadPanel
            onUploaded={async (title) => {
              setNotice(`已上传「${title}」。`)
              setDetail(null)
              await refresh()
            }}
            onUnauthorized={onUnauthorized}
          />
        )}

        <section className="card materials-card">
          <h2>资料列表</h2>

          {loading && <p className="muted">加载中…</p>}

          {!loading && materials.length === 0 && <p className="muted">本班还没有材料。</p>}

          {materials.length > 0 && (
            <ul className="materials">
              {materials.map((material) => (
                <li key={material.id}>
                  <div className="material-main">
                    <strong>{material.title}</strong>
                    <span className="muted">
                      {' '}
                      {material.original_filename} · {material.content_type} ·{' '}
                      {formatTimestamp(material.created_at)}
                    </span>
                  </div>
                  <div className="material-actions">
                    <button type="button" className="secondary" onClick={() => void openMaterial(material.id)}>
                      查看
                    </button>
                    <button type="button" className="secondary" onClick={() => void run(() => api.downloadMaterial(material.id, material.original_filename))}>
                      下载
                    </button>
                    {/* Index controls are a convenience for the teacher; the
                        server refuses a student's rebuild on its own. */}
                    {user.role === 'teacher' && (
                      <IndexPanel material={material} onUnauthorized={onUnauthorized} />
                    )}
                  </div>
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>

      {detail !== null && (
        <section className="card detail-card">
          <h2>{detail.material.title}</h2>
          <p className="muted">{detail.material.original_filename} · 提取文本</p>
          {/* Rendered as text, never as markup: material content is untrusted. */}
          <pre className="content">{detail.content}</pre>
        </section>
      )}
    </main>
  )
}

interface UploadPanelProps {
  onUploaded: (title: string) => void | Promise<void>
  onUnauthorized: () => void
}

function UploadPanel({ onUploaded, onUnauthorized }: UploadPanelProps) {
  const [title, setTitle] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [strategy, setStrategy] = useState<ChunkStrategy>(api.DEFAULT_CHUNK_STRATEGY)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()

    if (file === null) {
      setProblem('请选择要上传的文件。')
      return
    }

    setBusy(true)
    setProblem(null)

    try {
      await api.uploadMaterial(title, file, strategy)
      setTitle('')
      setFile(null)
      await onUploaded(title)
    } catch (failure) {
      // A parsing failure is reported as a failure; success is never faked.
      if (failure instanceof api.ApiError) {
        if (failure.status === 401) {
          onUnauthorized()
          return
        }
        setProblem(describeUploadFailure(failure))
      } else {
        setProblem('网络错误，上传未完成，请重试。')
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="card upload-card">
      <h2>上传材料</h2>
      <p className="muted">
        支持 {api.SUPPORTED_EXTENSIONS.join(' / ')}，单个文件不超过 5 MiB。
        PDF 与 DOCX 仅提取可读取的文本（含正文段落与表格文字），不执行 OCR，
        扫描件与加密文档会上传失败。
      </p>

      <form className="upload" onSubmit={handleSubmit}>
        <label htmlFor="title">标题</label>
        <input
          id="title"
          value={title}
          onChange={(event) => setTitle(event.target.value)}
          maxLength={255}
          required
        />

        <label htmlFor="file">文件</label>
        <input
          id="file"
          type="file"
          accept={ACCEPT}
          onChange={(event) => setFile(event.target.files?.[0] ?? null)}
          required
        />

        <ChunkStrategyFields value={strategy} onChange={setStrategy} idPrefix="upload" />

        {problem !== null && (
          <p className="error" role="alert">
            {problem}
          </p>
        )}

        <button type="submit" disabled={busy}>
          {busy ? '上传中…' : '上传'}
        </button>
      </form>
    </section>
  )
}

function describeUploadFailure(failure: api.ApiError): string {
  switch (failure.status) {
    case 415:
      return '不支持该文件类型，请选择 .md、.txt、.pdf 或 .docx。'
    case 413:
      return '文件或请求过大（单个文件上限 5 MiB）。'
    case 422:
      return '无法从该文档提取文本：可能是加密文件、扫描件或没有可提取的正文。'
    case 400:
      return '文件内容、文件名或切分参数不合法，上传已取消。'
    case 403:
      return '当前账号没有上传权限。'
    case 503:
      return '文档解析繁忙，请稍后重试。'
    default:
      return failure.message
  }
}

function formatTimestamp(raw: string): string {
  const parsed = new Date(raw)
  return Number.isNaN(parsed.getTime()) ? raw : parsed.toLocaleString()
}
