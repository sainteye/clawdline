import { useId, useRef, useState } from "react"
import { supportWord as w } from "../support/words.js"

type Props = {
  query: string
  status: string
  sort: string
  onSearch: (query: string) => void
  onStatus: (status: string) => void
  onSort: (sort: string) => void
  onReset: () => void
}

export function CommunityListControls(p: Props) {
  const id = useId()
  const [searchOpen, setSearchOpen] = useState(false)
  const [filtersOpen, setFiltersOpen] = useState(false)
  const [draft, setDraft] = useState(p.query)
  const searchTrigger = useRef<HTMLButtonElement>(null)
  const filterTrigger = useRef<HTMLButtonElement>(null)
  const count = Number(p.status !== "all") + Number(p.sort !== "activity")
  const filterLabel = count ? w("communityFiltersActive", {count: String(count)}) : w("communityFilters")
  const removeIcon = <svg width="13" height="13" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true"><path d="m5 5 10 10M15 5 5 15"/></svg>

  return <div className="community-list-controls">
    <div className="community-toolbar">
      <div className="community-active-filters" aria-live="polite">
        {p.query ? <button type="button" className="community-filter-chip" aria-label={w("communityRemoveFilter", {label: p.query})} title={p.query} onClick={() => {setDraft(""); p.onSearch("")}}><span>{p.query}</span>{removeIcon}</button> : null}
        {p.status !== "all" ? <button type="button" className="community-filter-chip" aria-label={w("communityRemoveFilter", {label: w(p.status === "open" ? "communityOpen" : "communityClosed")})} onClick={() => p.onStatus("all")}><span>{w(p.status === "open" ? "communityOpen" : "communityClosed")}</span>{removeIcon}</button> : null}
        {p.sort !== "activity" ? <button type="button" className="community-filter-chip" aria-label={w("communityRemoveFilter", {label: w("communityNewest")})} onClick={() => p.onSort("activity")}><span>{w("communityNewest")}</span>{removeIcon}</button> : null}
      </div>
      <div className="community-tools">
        <button ref={searchTrigger} type="button" className="community-tool" aria-label={w("communitySearch")} title={w("communitySearch")} aria-expanded={searchOpen} aria-controls={searchOpen ? id + "-search" : undefined} data-active={!!p.query || searchOpen} onClick={() => setSearchOpen(open => !open)}><svg width="19" height="19" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" aria-hidden="true"><circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 5 5"/></svg></button>
        <button ref={filterTrigger} type="button" className="community-tool" aria-label={filterLabel} title={filterLabel} aria-expanded={filtersOpen} aria-controls={filtersOpen ? id + "-filters" : undefined} data-active={count > 0 || filtersOpen} onClick={() => setFiltersOpen(open => !open)}><svg width="19" height="19" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M3 5h18l-7 8v6l-4 2v-8Z"/></svg>{count ? <span className="community-filter-count" aria-hidden="true">{count}</span> : null}</button>
      </div>
    </div>
    {searchOpen ? <form id={id + "-search"} className="community-search-panel" role="search" aria-label={w("communitySearch")} onSubmit={e => {e.preventDefault(); p.onSearch(draft)}} onKeyDown={e => {if(e.key === "Escape") {e.preventDefault(); e.stopPropagation(); setSearchOpen(false); searchTrigger.current?.focus()}}}>
      <div className="community-search"><label htmlFor={id + "-query"}>{w("communitySearch")}</label><div className="help-search-row"><input autoFocus id={id + "-query"} type="search" value={draft} placeholder={w("communitySearchHint")} onChange={e => setDraft(e.target.value)}/><button className="help-btn" type="submit">{w("searchGo")}</button></div></div>
    </form> : null}
    {filtersOpen ? <div id={id + "-filters"} className="community-filter-panel" role="region" aria-label={w("communityFilters")} onKeyDown={e => {if(e.key === "Escape" && !e.defaultPrevented) {e.preventDefault(); e.stopPropagation(); setFiltersOpen(false); filterTrigger.current?.focus()}}}>
      <label>{w("communityFilterStatus")}<select value={p.status} onChange={e => p.onStatus(e.target.value)}><option value="all">{w("allStates")}</option><option value="open">{w("communityOpen")}</option><option value="closed">{w("communityClosed")}</option></select></label>
      <label>{w("communitySortBy")}<select value={p.sort} onChange={e => p.onSort(e.target.value)}><option value="activity">{w("communityActivity")}</option><option value="new">{w("communityNewest")}</option></select></label>
      {count ? <button type="button" className="community-filter-reset" onClick={p.onReset}>{w("communityResetFilters")}</button> : null}
    </div> : null}
  </div>
}
