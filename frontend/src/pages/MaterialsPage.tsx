import { useCallback, useEffect, useRef, useState, type FormEvent, type KeyboardEvent as ReactKeyboardEvent } from 'react'
import * as api from '../api'
import type { ChunkStrategy, Material, MaterialDetail, User } from '../api'
import Brand from '../components/Brand'
import ChunkStrategyFields from '../components/ChunkStrategyFields'
import IndexPanel from '../components/IndexPanel'
import RetrievalPanel from '../components/RetrievalPanel'

interface Props {
  user: User
  onSignOut: () => void
  onUnauthorized: () => void
}

type Workspace = 'materials' | 'ask' | 'search'
type ContextPanel = { kind: 'reader'; id: string } | { kind: 'index'; material: Material }
const WORKSPACES: { id: Workspace; label: string; description: string }[] = [
  { id: 'materials', label: '材料', description: '查看、下载与管理本班资料' },
  { id: 'ask', label: '问答', description: '根据材料回答问题并核对出处' },
  { id: 'search', label: '检索', description: '定位材料中的相关段落' },
]
const ACCEPT = api.SUPPORTED_EXTENSIONS.join(',')

export default function MaterialsPage({ user, onSignOut, onUnauthorized }: Props) {
  const [materials, setMaterials] = useState<Material[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [workspace, setWorkspace] = useState<Workspace>('materials')
  const [uploadOpen, setUploadOpen] = useState(false)
  const [panel, setPanel] = useState<ContextPanel | null>(null)
  const [detail, setDetail] = useState<MaterialDetail | null>(null)
  const [detailBusy, setDetailBusy] = useState(false)
  const [detailError, setDetailError] = useState<string | null>(null)
  const tabRefs = useRef<Record<Workspace, HTMLButtonElement | null>>({ materials: null, ask: null, search: null })
  const positions = useRef<Record<Workspace, number>>({ materials: 0, ask: 0, search: 0 })
  const opener = useRef<HTMLElement | null>(null)
  const closeRef = useRef<HTMLButtonElement | null>(null)
  const requestId = useRef(0)

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

  useEffect(() => { void refresh() }, [refresh])

  useEffect(() => {
    if (panel === null) return
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    closeRef.current?.focus()
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') closePanel()
      if (event.key !== 'Tab') return
      const controls = Array.from(document.querySelectorAll<HTMLElement>('.context-sheet button:not(:disabled), .context-sheet input:not(:disabled), .context-sheet select:not(:disabled)'))
      const first = controls[0]
      const last = controls[controls.length - 1]
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last?.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first?.focus()
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.body.style.overflow = previousOverflow
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [panel])

  function changeWorkspace(next: Workspace) {
    if (next === workspace) return
    positions.current[workspace] = window.scrollY
    setWorkspace(next)
    setTimeout(() => {
      try { window.scrollTo(0, positions.current[next]) } catch { /* jsdom has no scrolling */ }
    }, 0)
  }

  function onTabKeyDown(event: ReactKeyboardEvent<HTMLButtonElement>, current: Workspace) {
    const index = WORKSPACES.findIndex((item) => item.id === current)
    let next = index
    if (event.key === 'ArrowRight') next = (index + 1) % WORKSPACES.length
    else if (event.key === 'ArrowLeft') next = (index + WORKSPACES.length - 1) % WORKSPACES.length
    else if (event.key === 'Home') next = 0
    else if (event.key === 'End') next = WORKSPACES.length - 1
    else return
    event.preventDefault()
    const target = WORKSPACES[next].id
    changeWorkspace(target)
    tabRefs.current[target]?.focus()
  }

  async function handleSignOut() {
    try {
      await api.logout()
      onSignOut()
    } catch (failure) {
      if (failure instanceof api.ApiError && failure.status === 401) onSignOut()
      else setError(failure instanceof api.ApiError ? failure.message : '网络错误，请重试。')
    }
  }

  function closePanel() {
    requestId.current++
    setPanel(null)
    setTimeout(() => opener.current?.focus(), 0)
  }

  function openIndex(material: Material) {
    opener.current = document.activeElement as HTMLElement
    setPanel({ kind: 'index', material })
  }

  async function openMaterial(materialId: string) {
    opener.current = document.activeElement as HTMLElement
    const currentRequest = ++requestId.current
    setPanel({ kind: 'reader', id: materialId })
    setDetail(null)
    setDetailError(null)
    setDetailBusy(true)
    try {
      const loaded = await api.getMaterial(materialId)
      if (requestId.current === currentRequest) setDetail(loaded)
    } catch (failure) {
      if (requestId.current !== currentRequest) return
      if (failure instanceof api.ApiError && failure.status === 401) {
        onUnauthorized()
        return
      }
      setDetailError(failure instanceof api.ApiError ? failure.message : '网络错误，请重试。')
    } finally {
      if (requestId.current === currentRequest) setDetailBusy(false)
    }
  }

  return (
    <main className="app materials-page">
      <header className="bar site-header">
        <Brand />
        <div>
          <strong>{user.username}</strong>
          <span className="muted">
            {' '}· {user.role === 'teacher' ? '教师' : '学生'} · {user.class_name}（班级 ID {user.class_id}）
          </span>
        </div>
        <button type="button" className="secondary" onClick={handleSignOut}>登出</button>
      </header>

      <div className="page-intro">
        <p className="eyebrow">CLASS MATERIALS</p>
        <h1>本班材料</h1>
        <p className="muted">整理与分享课程资料，让班级学习更有条理。</p>
      </div>

      {error !== null && (
        <div className="page-message">
          <p className="error" role="alert">{error}</p>
          <button type="button" className="secondary" onClick={() => void refresh()}>重试</button>
        </div>
      )}
      {notice !== null && <p className="notice page-message" role="status">{notice}</p>}

      <nav className="workspace-nav" role="tablist" aria-label="材料工作区">
        {WORKSPACES.map((item) => (
          <button
            key={item.id}
            ref={(node) => { tabRefs.current[item.id] = node }}
            type="button"
            id={'workspace-tab-' + item.id}
            role="tab"
            aria-controls={'workspace-panel-' + item.id}
            aria-selected={workspace === item.id}
            tabIndex={workspace === item.id ? 0 : -1}
            onClick={() => changeWorkspace(item.id)}
            onKeyDown={(event) => onTabKeyDown(event, item.id)}
          >
            <span>{item.label}</span>
            <small>{item.description}</small>
          </button>
        ))}
      </nav>

      <section id="workspace-panel-materials" role="tabpanel" aria-labelledby="workspace-tab-materials" hidden={workspace !== 'materials'} className="workspace-panel materials-workspace">
        {user.role === 'teacher' && (
          <div className="materials-toolbar">
            <p className="muted">本班的课程文件与提取文本</p>
            <button type="button" aria-expanded={uploadOpen} aria-controls="upload-panel" onClick={() => setUploadOpen((open) => !open)}>
              {uploadOpen ? '收起上传' : '上传材料'}
            </button>
          </div>
        )}
        {user.role === 'teacher' && uploadOpen && (
          <div id="upload-panel">
            <UploadPanel
              onUploaded={async (title) => {
                setNotice('已上传「' + title + '」。')
                setUploadOpen(false)
                await refresh()
              }}
              onUnauthorized={onUnauthorized}
            />
          </div>
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
                      {' '}{material.original_filename} · {material.content_type} · {formatTimestamp(material.created_at)}
                    </span>
                  </div>
                  <div className="material-actions">
                    <button type="button" className="secondary" onClick={() => void openMaterial(material.id)}>查看</button>
                    <button type="button" className="secondary" onClick={() => void run(() => api.downloadMaterial(material.id, material.original_filename))}>下载</button>
                    {user.role === 'teacher' && (
                      <button type="button" className="secondary" onClick={() => openIndex(material)}>索引</button>
                    )}
                  </div>
                </li>
              ))}
            </ul>
          )}
        </section>
      </section>

      <RetrievalPanel
        activeWorkspace={workspace}
        onUnauthorized={onUnauthorized}
        onOpenMaterial={(materialId) => void openMaterial(materialId)}
      />

      {panel !== null && (
        <div className="context-overlay" onMouseDown={(event) => { if (event.target === event.currentTarget) closePanel() }}>
          <section className="context-sheet detail-card" role="dialog" aria-modal="true" aria-labelledby="context-title">
            <div className="context-header">
              <div>
                <p className="eyebrow">{panel.kind === 'reader' ? 'MATERIAL READER' : 'MATERIAL INDEX'}</p>
                <h2 id="context-title">{panel.kind === 'reader' ? (detail?.material.title ?? '材料正文') : panel.material.title}</h2>
              </div>
              <button ref={closeRef} type="button" className="secondary" onClick={closePanel}>关闭</button>
            </div>
            {panel.kind === 'reader' ? (
              <>
                {detailBusy && <p className="muted" role="status">读取中…</p>}
                {detailError !== null && <p className="error" role="alert">{detailError}</p>}
                {detail !== null && (
                  <>
                    <p className="muted">{detail.material.original_filename} · 提取文本</p>
                    <pre className="content">{detail.content}</pre>
                  </>
                )}
              </>
            ) : (
              <IndexPanel key={panel.material.id} material={panel.material} onUnauthorized={onUnauthorized} />
            )}
          </section>
        </div>
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
