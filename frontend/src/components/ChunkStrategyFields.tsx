import * as api from '../api'
import type { ChunkStrategy } from '../api'

interface Props {
  value: ChunkStrategy
  onChange: (next: ChunkStrategy) => void
  /** Keeps the generated element ids unique when two forms share a page. */
  idPrefix: string
}

/**
 * ChunkStrategyFields is the optional split configuration for an upload or a
 * rebuild.
 *
 * The length, overlap, separator and preprocessing controls only appear for the
 * custom strategy, because the server ignores them for the other two: showing
 * them would suggest a setting that has no effect.
 */
export default function ChunkStrategyFields({ value, onChange, idPrefix }: Props) {
  function update(patch: Partial<ChunkStrategy>) {
    onChange({ ...value, ...patch })
  }

  return (
    <div className="chunk-strategy">
      <div className="field">
        <label htmlFor={`${idPrefix}-strategy`}>切分策略</label>
        <select
          id={`${idPrefix}-strategy`}
          value={value.strategy}
          onChange={(event) => update({ strategy: event.target.value as ChunkStrategy['strategy'] })}
        >
          {api.CHUNK_STRATEGIES.map((option) => (
            <option key={option} value={option}>
              {api.CHUNK_STRATEGY_LABELS[option]}
            </option>
          ))}
        </select>
      </div>

      {value.strategy === 'custom' && (
        <div className="chunk-custom">
          <div className="field">
            <label htmlFor={`${idPrefix}-max-chars`}>最大长度（100–2000 字）</label>
            <input
              id={`${idPrefix}-max-chars`}
              type="number"
              min={100}
              max={2000}
              value={value.maxChars}
              onChange={(event) => update({ maxChars: event.target.value })}
            />
          </div>

          <div className="field">
            <label htmlFor={`${idPrefix}-overlap`}>重叠比例（0–50%）</label>
            <input
              id={`${idPrefix}-overlap`}
              type="number"
              min={0}
              max={50}
              value={value.overlapPercent}
              onChange={(event) => update({ overlapPercent: event.target.value })}
            />
          </div>

          <div className="field">
            <label htmlFor={`${idPrefix}-separator`}>优先断点</label>
            <select
              id={`${idPrefix}-separator`}
              value={value.separator}
              onChange={(event) => update({ separator: event.target.value })}
            >
              {api.CHUNK_SEPARATORS.map((option) => (
                <option key={option} value={option}>
                  {api.CHUNK_SEPARATOR_LABELS[option]}
                </option>
              ))}
            </select>
          </div>

          <div className="field checkbox-field">
            <label htmlFor={`${idPrefix}-remove-urls`}>
              <input
                id={`${idPrefix}-remove-urls`}
                type="checkbox"
                checked={value.removeUrlsEmails}
                onChange={(event) => update({ removeUrlsEmails: event.target.checked })}
              />
              移除 URL 与邮箱
            </label>
          </div>

          <div className="field checkbox-field">
            <label htmlFor={`${idPrefix}-fold-whitespace`}>
              <input
                id={`${idPrefix}-fold-whitespace`}
                type="checkbox"
                checked={value.foldWhitespace}
                onChange={(event) => update({ foldWhitespace: event.target.checked })}
              />
              折叠连续空白
            </label>
          </div>
        </div>
      )}
    </div>
  )
}
