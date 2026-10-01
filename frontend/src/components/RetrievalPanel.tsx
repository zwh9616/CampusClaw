import { useEffect, useState, type FormEvent } from 'react'
import * as api from '../api'
import type { AnswerResult, SearchHit, SearchMode, SearchResult } from '../api'

interface Props {
  activeWorkspace: 'materials' | 'ask' | 'search'
  /** Called when the server reports the session is gone. */
  onUnauthorized: () => void
  /** Opens one of the class's materials, still subject to the server's class check. */
  onOpenMaterial: (materialId: string) => void
}

const BASIS_LABEL: Record<SearchHit['offset_basis'], string> = {
  extracted: '原文位置',
  normalized: '预处理后位置',
}

/**
 * describe turns a failure into something a reader can act on.
 *
 * An outage is presented as retryable and a bad query as fixable; neither is
 * allowed to look like an empty result, which would read as "the material does
 * not mention this".
 */
function describe(failure: unknown): string {
  if (failure instanceof api.ApiError) {
    switch (failure.status) {
      case 400:
        return '查询不合法，请修改后重试。'
      case 503:
        return '检索服务暂不可用，请稍后重试。'
      default:
        return failure.message
    }
  }
  return '网络错误，请重试。'
}

/**
 * RetrievalPanel is the search and question area of the materials page.
 *
 * It is the same panel for both roles: a student and a teacher search the same
 * class material, and the only difference between them is the teacher-only
 * controls rendered elsewhere.
 */
export default function RetrievalPanel({ activeWorkspace, onUnauthorized, onOpenMaterial }: Props) {
  const [mode, setMode] = useState<SearchMode>('hybrid')
  const [query, setQuery] = useState('')
  const [result, setResult] = useState<SearchResult | null>(null)
  const [searching, setSearching] = useState(false)
  const [searchProblem, setSearchProblem] = useState<string | null>(null)

  const [question, setQuestion] = useState('')
  const [answer, setAnswer] = useState<AnswerResult | null>(null)
  const [asking, setAsking] = useState(false)
  const [askProblem, setAskProblem] = useState<string | null>(null)
  const [activeCitation, setActiveCitation] = useState<number | null>(null)
  const [searchVersion, setSearchVersion] = useState(0)
  const [answerVersion, setAnswerVersion] = useState(0)

  async function handleSearch(event: FormEvent) {
    event.preventDefault()

    if (query.trim() === '') {
      setSearchProblem('请输入要检索的内容。')
      setResult(null)
      return
    }

    setSearching(true)
    setSearchProblem(null)

    try {
      setResult(await api.search(query, mode))
      setSearchVersion((version) => version + 1)
    } catch (failure) {
      if (failure instanceof api.ApiError && failure.status === 401) {
        onUnauthorized()
        return
      }
      setResult(null)
      setSearchProblem(describe(failure))
    } finally {
      setSearching(false)
    }
  }

  async function handleAsk(event: FormEvent) {
    event.preventDefault()

    if (question.trim() === '') {
      setAskProblem('请输入问题。')
      setAnswer(null)
      return
    }

    setAsking(true)
    setAskProblem(null)
    setActiveCitation(null)

    try {
      setAnswer(await api.ask(question))
      setAnswerVersion((version) => version + 1)
    } catch (failure) {
      if (failure instanceof api.ApiError && failure.status === 401) {
        onUnauthorized()
        return
      }
      setAnswer(null)
      setAskProblem(describe(failure))
    } finally {
      setAsking(false)
    }
  }

  return (
    <div className="retrieval-area" hidden={activeWorkspace === 'materials'}>
      <section id="workspace-panel-search" role="tabpanel" aria-labelledby="workspace-tab-search" hidden={activeWorkspace !== 'search'} className="card retrieval-card">
        <h2>检索本班材料</h2>
        <p className="muted">
          在已保存的材料正文中查找相关段落，并给出可以打开核对的出处。
        </p>

        <form className="search" onSubmit={handleSearch}>
          <div className="field">
            <label htmlFor="search-query">查询内容</label>
            <input
              id="search-query"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="例如：向量检索的基本原理"
            />
          </div>

          <div className="field">
            <label htmlFor="search-mode">检索方式</label>
            <select
              id="search-mode"
              value={mode}
              onChange={(event) => setMode(event.target.value as SearchMode)}
            >
              {api.SEARCH_MODES.map((option) => (
                <option key={option} value={option}>
                  {api.SEARCH_MODE_LABELS[option]}
                </option>
              ))}
            </select>
          </div>

          {searchProblem !== null && (
            <p className="error" role="alert">{searchProblem}</p>
          )}

          <button type="submit" disabled={searching}>
            {searching ? '检索中…' : '检索'}
          </button>
        </form>

        {result !== null && (
          <SearchResults
            key={searchVersion}
            result={result}
            onOpenMaterial={onOpenMaterial}
          />
        )}

      </section>

      <section id="workspace-panel-ask" role="tabpanel" aria-labelledby="workspace-tab-ask" hidden={activeWorkspace !== 'ask'} className="card ask-card">
        <h2>简短问答</h2>
        <p className="muted">
          只有本班材料中有依据时才会回答；回答中的编号与下方出处一一对应。
        </p>

        <form className="ask" onSubmit={handleAsk}>
          <div className="field">
            <label htmlFor="ask-question">问题</label>
            <input
              id="ask-question"
              value={question}
              onChange={(event) => setQuestion(event.target.value)}
              placeholder="例如：这门课讲了哪些检索方式？"
            />
          </div>

          {askProblem !== null && (
            <p className="error" role="alert">{askProblem}</p>
          )}

          <button type="submit" disabled={asking}>
            {asking ? '生成中…' : '提问'}
          </button>
        </form>

        {answer !== null && (
          <Answer
            key={answerVersion}
            answer={answer}
            active={activeCitation}
            onSelect={setActiveCitation}
            onOpenMaterial={onOpenMaterial}
          />
        )}
      </section>
    </div>
  )
}

function SearchResults({ result, onOpenMaterial }: {
  result: SearchResult
  onOpenMaterial: (materialId: string) => void
}) {
  const hits = result.hits ?? []

  // An empty result is a result: it says the class material does not cover the
  // query, which is different from a failed request.
  if (hits.length === 0) {
    return (
      <p className="notice" role="status">
        {result.message ?? '资料中未找到相关内容'}
      </p>
    )
  }

  return (
    <ol className="hits">
      {hits.map((hit) => (
        <li key={hit.chunk_id} className="hit">
          <div className="hit-head">
            <strong>{hit.title}</strong>
            <span className="muted">
              切片 {hit.chunk_index + 1} · 字符 {hit.start_offset}–{hit.end_offset} ·{' '}
              {BASIS_LABEL[hit.offset_basis]}
            </span>
          </div>

          {/* Plain text: material content is untrusted and is never markup. */}
          <Excerpt text={hit.excerpt} />

          <HitDiagnostics hit={hit} />

          <div className="hit-actions">
            <button
              type="button"
              className="secondary"
              onClick={() => onOpenMaterial(hit.material_id)}
            >
              打开材料
            </button>
          </div>
        </li>
      ))}
    </ol>
  )
}

function Excerpt({ text, revealToken = 0 }: { text: string; revealToken?: number }) {
  const [expanded, setExpanded] = useState(false)
  useEffect(() => { if (revealToken > 0) setExpanded(true) }, [revealToken])
  const chars = Array.from(text)
  const long = chars.length > 180
  const shown = long && !expanded ? chars.slice(0, 180).join('') + '…' : text
  return (
    <div className="excerpt-block">
      <p className="excerpt">{shown}</p>
      {long && (
        <button type="button" className="excerpt-toggle" aria-expanded={expanded} onClick={() => setExpanded((value) => !value)}>
          {expanded ? '收起原文' : '展开原文'}
        </button>
      )}
    </div>
  )
}

/**
 * HitDiagnostics shows how each retrieval path ranked a hit.
 *
 * It reports ranks and scores only — never a vector component, which the API
 * does not send and the page must not display.
 */
function HitDiagnostics({ hit }: { hit: SearchHit }) {
  const parts: string[] = []

  if (hit.keyword_rank !== null) {
    parts.push(`关键字 #${hit.keyword_rank}`)
  }
  if (hit.vector_rank !== null) {
    parts.push(`语义 #${hit.vector_rank}`)
  }
  if (hit.rrf_score !== null) {
    parts.push(`融合 ${hit.rrf_score.toFixed(4)}`)
  }

  if (parts.length === 0) return null

  return <p className="diagnostics muted">{parts.join(' · ')}</p>
}

function Answer({ answer, active, onSelect, onOpenMaterial }: {
  answer: AnswerResult
  active: number | null
  onSelect: (position: number) => void
  onOpenMaterial: (materialId: string) => void
}) {
  const citations = answer.citations ?? []
  const [selectionToken, setSelectionToken] = useState(0)
  function selectCitation(position: number) {
    onSelect(position)
    setSelectionToken((token) => token + 1)
  }
  useEffect(() => {
    if (active === null) return
    const target = document.getElementById('citation-' + (active + 1))
    target?.scrollIntoView?.({ behavior: 'smooth', block: 'nearest' })
    target?.focus({ preventScroll: true })
  }, [active, selectionToken])

  return (
    <div className="answer-block">
      <AnswerText text={answer.answer} citations={citations} onSelect={selectCitation} />

      {citations.length === 0 ? (
        <p className="muted">本题没有可引用的本班材料。</p>
      ) : (
        <ol className="citations">
          {citations.map((citation, position) => (
            <li
              key={citation.chunk_id}
              id={`citation-${position + 1}`}
              tabIndex={-1}
              className={active === position ? 'citation citation-active' : 'citation'}
            >
              <div className="hit-head">
                <strong>[{position + 1}] {citation.title}</strong>
                <span className="muted">
                  切片 {citation.chunk_index + 1} · 字符 {citation.start_offset}–{citation.end_offset}
                </span>
              </div>
              <Excerpt text={citation.excerpt} revealToken={active === position ? selectionToken : 0} />
              <div className="hit-actions">
                <button
                  type="button"
                  className="secondary"
                  onClick={() => onOpenMaterial(citation.material_id)}
                >
                  打开材料
                </button>
              </div>
            </li>
          ))}
        </ol>
      )}
    </div>
  )
}

/**
 * AnswerText renders the answer with its citation markers.
 *
 * A marker that names a slice the server did not supply is left as plain text:
 * it must not become a control that appears to point at a source.
 */
function AnswerText({ text, citations, onSelect }: {
  text: string
  citations: SearchHit[]
  onSelect: (position: number) => void
}) {
  const segments = text.split(/(\[\d+\])/g)

  return (
    <p className="answer">
      {segments.map((segment, index) => {
        const marker = /^\[(\d+)\]$/.exec(segment)

        if (marker === null) {
          return <span key={index}>{segment}</span>
        }

        const number = Number(marker[1])

        if (number < 1 || number > citations.length) {
          return <span key={index}>{segment}</span>
        }

        return (
          <button
            key={index}
            type="button"
            className="citation-link"
            aria-label={`查看出处 ${number}`}
            onClick={() => onSelect(number - 1)}
          >
            {segment}
          </button>
        )
      })}
    </p>
  )
}
