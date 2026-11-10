import { useEffect, useRef } from 'react'
import { onEngineEvent } from '../services/api'

/** Subscribe to an engine event topic for the lifetime of the component. */
export function useEngineEvent<T>(topic: string, handler: (data: T) => void): void {
  const ref = useRef(handler)
  ref.current = handler
  useEffect(() => onEngineEvent(topic, (d) => ref.current(d as T)), [topic])
}
