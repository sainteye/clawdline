export function ViewPluginIcon({ className }: { className?: string }) {
  return <svg className={className} viewBox="0 0 32 32" fill="none" aria-hidden="true">
    <ellipse cx="16" cy="16" rx="14" ry="6" transform="rotate(-30 16 16)" stroke="currentColor" strokeWidth="1.3" />
    <ellipse cx="16" cy="16" rx="13" ry="6" transform="rotate(40 16 16)" stroke="currentColor" strokeWidth="1" />
    <circle cx="16" cy="16" r="3" fill="currentColor" /><circle cx="27" cy="10" r="2" fill="currentColor" />
  </svg>
}
export function ViewArtwork({ id, small = false }: { id: string; small?: boolean }) {
  const galaxy = id === "com.clawdline.session-galaxy"
  return <div className={`vp-art ${galaxy ? "vp-art-galaxy" : "vp-art-panels"}${small ? " vp-art-small" : ""}`} aria-hidden="true">
    {galaxy ? <><i className="vp-art-orbit" /><i className="vp-art-orbit vp-art-orbit-two" /><i className="vp-art-core" /><i className="vp-art-star" /><i className="vp-art-star vp-art-star-two" /></> :
      <><i className="vp-art-window"><b /><b /><b /></i><i className="vp-art-window vp-art-window-back"><b /><b /></i></>}
  </div>
}
