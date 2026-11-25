import { useCallback, useEffect, useState } from "react"
import { ModelCatalogReader, type AgentModels, type ModelCatalogSource } from "./reader"

export type { AgentModels, ModelCatalogSource } from "./reader"

export type ModelCatalog = {
  /** One agent's list: loading, ready (possibly still waiting on the agent), failed or unsupported. */
  get: (agent: string) => AgentModels
  /** Asks the machine again; a picker's Retry calls it. */
  retry: () => void
}

/**
 * The machine's model catalog for every picker that offers a model for an
 * agent. One read answers every agent; while the daemon is still asking an
 * agent, the hook polls and redraws. Pass the captured machine source, local
 * or Cloud; a source without the catalog yields "unsupported".
 */
export function useModelCatalog(source: ModelCatalogSource | undefined): ModelCatalog {
  const [reader, setReader] = useState<ModelCatalogReader>()
  const [, redraw] = useState(0)
  useEffect(() => {
    const next = new ModelCatalogReader(source, () => redraw(n => n + 1))
    setReader(next)
    next.start()
    return () => next.stop()
  }, [source])
  const get = useCallback((agent: string): AgentModels => reader?.get(agent) ?? { status: "loading" }, [reader])
  const retry = useCallback(() => reader?.retry(), [reader])
  return { get, retry }
}
