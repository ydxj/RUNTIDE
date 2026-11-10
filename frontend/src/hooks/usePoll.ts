import { useCallback, useEffect, useRef, useState } from 'react'
import { useUI } from '../stores/ui'

export interface PollState<T> {
  data: T | undefined
  error: string | undefined
  loading: boolean
  reload: () => Promise<void>
}

/**
 * Polls `fn` on the user's refresh interval. Polling pauses while the window
 * is hidden so RUNTIDE stays light when it lives in the tray.
 * Pass intervalSec=0 to load once.
 */
export function usePoll<T>(
  fn: () => Promise<T>,
  deps: unknown[] = [],
  intervalSec?: number,
): PollState<T> {
  const refresh = useUI((s) => s.settings?.refreshSec ?? 3)
  const every = intervalSec ?? refresh
  const [data, setData] = useState<T>()
  const [error, setError] = useState<string>()
  const [loading, setLoading] = useState(true)
  const fnRef = useRef(fn)
  fnRef.current = fn
  const alive = useRef(true)
  const inflight = useRef(false)

  const reload = useCallback(async () => {
    if (inflight.current) return
    inflight.current = true
    try {
      const v = await fnRef.current()
      if (alive.current) {
        setData(v)
        setError(undefined)
      }
    } catch (e) {
      if (alive.current) setError(e instanceof Error ? e.message : String(e))
    } finally {
      inflight.current = false
      if (alive.current) setLoading(false)
    }
  }, [])

  useEffect(() => {
    alive.current = true
    setLoading(true)
    void reload()
    if (every <= 0) return () => void (alive.current = false)
    const t = setInterval(() => {
      if (!document.hidden) void reload()
    }, every * 1000)
    return () => {
      alive.current = false
      clearInterval(t)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [every, reload, ...deps])

  return { data, error, loading, reload }
}
