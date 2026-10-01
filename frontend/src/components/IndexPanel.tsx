import { useState } from 'react'
import * as api from '../api'
import type { ChunkStrategy, IndexState, Material } from '../api'
import ChunkStrategyFields from './ChunkStrategyFields'

interface Props {
  material: Material
  /** Called when the server reports the session is gone. */
  onUnauthorized: () => void
}

const STATUS_LABEL: Record<IndexStatusKey, string> = {
  none: '尚未建立索引',
  pending: '索引中',
  ready: '已就绪',
  failed: '索引失败，可重试',
}

type IndexStatusKey = api.IndexStatus

/**
 * IndexPanel is the teacher's per-material index view and rebuild control.
 *
 * It fetches nothing until it is opened, so listing a class's material does not
 * fan out into one status request per row. The controls here are a convenience,
 * not the authorisation: the server refuses a student's rebuild request on its
 * own.
 */
export default function IndexPanel({ material, onUnauthorized }: Props) {
  const [open, setOpen] = useState(false)
  const [state, setState] = useState<IndexState | null>(null)
  const [strategy, setStrategy] = useState<ChunkStrategy>(api.DEFAULT_CHUNK_STRATEGY)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  async function handleToggle() {
    if (open) {
      setOpen(false)
      return
    }

    setOpen(true)
    await load()
  }

  async function load() {
    setBusy(true)
    setProblem(null)

    try {
      setState(await api.getIndexState(material.id))
    } catch (failure) {
      if (failure instanceof api.ApiError && failure.status === 401) {
        onUnauthorized()
        return
      }
      setProblem(describe(failure))
    } finally {
      setBusy(false)
    }
  }

  async function rebuild() {
    setBusy(true)
    setProblem(null)

    try {
      setState(await api.reindexMaterial(material.id, strategy))
    } catch (failure) {
      if (failure instanceof api.ApiError && failure.status === 401) {
        onUnauthorized()
        return
      }
      setProblem(describe(failure))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="index-panel">
      <button type="button" className="secondary" onClick={() => void handleToggle()}>
        {open ? '收起索引' : '索引'}
      </button>

      {open && (
        <div className="index-body">
          {busy && state === null && <p className="muted">读取中…</p>}

          {state !== null && (
            <p className="index-status">
              <span className={`status-chip status-${state.status}`}>
                {STATUS_LABEL[state.status] ?? state.status}
              </span>
              {state.status !== 'none' && (
                <span className="muted">
                  {' '}
                  第 {state.generation} 代 · 策略 {state.strategy} · 上限 {state.chunk_max_chars} 字 ·
                  重叠 {state.chunk_overlap_percent}%
                </span>
              )}
            </p>
          )}

          {state !== null && state.status === 'failed' && state.failure_code !== '' && (
            <p className="muted">失败原因代码：{state.failure_code}</p>
          )}

          <ChunkStrategyFields value={strategy} onChange={setStrategy} idPrefix={`reindex-${material.id}`} />

          {problem !== null && (
            <p className="error" role="alert">{problem}</p>
          )}

          <button type="button" onClick={() => void rebuild()} disabled={busy}>
            {busy ? '处理中…' : state?.status === 'failed' ? '重试索引' : '重建索引'}
          </button>
        </div>
      )}
    </div>
  )
}

function describe(failure: unknown): string {
  if (failure instanceof api.ApiError) {
    switch (failure.status) {
      case 400:
        return '切分参数不合法，请修改后重试。'
      case 503:
        return '索引服务暂不可用，请稍后重试。'
      default:
        return failure.message
    }
  }
  return '网络错误，请重试。'
}
