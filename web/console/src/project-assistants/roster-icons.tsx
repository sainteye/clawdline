const paths = {
  pin:"M6 2h8l-2 3v4l3 3H5l3-3V5L6 2Zm4 10v6",
  section:"M2 8l3-4h10l3 4v7a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1V8Zm0 1h5l1 3h4l1-3h5",
  read:"m3 10 4 4 9-9",
  edit:"M10 4H4v12h12v-6M8 12l1-4 7-6 2 2-7 7-3 1Z",
  copy:"M7 7h10v10H7V7ZM3 13V3h10",
  renew:"M5 8a6 6 0 1 1-1 5M5 2v6h6",
  lead:"m10 2.5 2.3 4.8 5.2.7-3.8 3.7.9 5.2-4.6-2.5-4.6 2.5.9-5.2L2.5 8l5.2-.7L10 2.5Z",
  hide:"M2 10s3-5 8-5 8 5 8 5-3 5-8 5-8-5-8-5ZM3 3l14 14",
  delete:"M3 5h14M7 5V3h6v2M5 5l1 12h8l1-12M8.5 8v6m3-6v6",
} as const

export function rosterIcon(name:keyof typeof paths) {
  return <svg viewBox="0 0 20 20" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.55" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={paths[name]}/></svg>
}
