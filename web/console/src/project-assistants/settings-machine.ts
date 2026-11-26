/**
 * The machine Bots settings open on: the one the person filtered to or whose
 * Bot is selected, otherwise the machine running the most Bots. Opening on
 * whichever source is listed first put a phone on an old test machine that
 * cannot share settings while the person's Bots ran elsewhere.
 */
export function settingsMachine(sources: readonly string[], botMachines: readonly string[], chosen: string | undefined): string | undefined {
  if (chosen && sources.includes(chosen)) return chosen
  let best: string | undefined, most = 0
  for (const id of sources) {
    const n = botMachines.filter(m => m === id).length
    if (n > most) { best = id; most = n }
  }
  return best ?? sources[0]
}
