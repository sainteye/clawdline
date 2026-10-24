import { useEffect } from "react"
import { catalogWord, catalogWordLanguage } from "../catalog.js"
import type { PageModule } from "./types.js"
import { transparencyURL } from "./transparency/public-url.js"
function TransparencyPage({ shown }: { shown: boolean }) {
  const url = transparencyURL(catalogWordLanguage("transparency", "title"), location.hash)
  useEffect(() => { if (shown) location.replace(url) }, [shown, url])
  if (!shown) return null
  return <section className="page"><a href={url}>{catalogWord("transparency", "title")}</a></section>
}
export const page: PageModule = { id: "transparency", Component: TransparencyPage }
