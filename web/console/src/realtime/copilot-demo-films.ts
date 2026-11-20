/**
 * The voice copilot's demonstration films, served from Clawdline's own media
 * host (`media.clawdline.com`, R2 behind its CDN). A person with no OpenAI key
 * yet can watch one instead of a call. Each name carries its content's hash,
 * so a new cut is a new address. Nothing is imported here, so `node --test`
 * loads it as it is.
 */

export interface DemoFilm { video: string; poster?: string }

const MEDIA = 'https://media.clawdline.com/voice-copilot/'

export const DEMO_FILMS: Record<'desktop' | 'phone', Record<'en' | 'zh-Hant', DemoFilm | null>> = {
  desktop: {
    // 1920×1080, 110.8 s and 163.1 s, published and checked on 2026-11-20.
    'en': { video: MEDIA + 'demo-en-d2857cd36962.mp4', poster: MEDIA + 'poster-en-1a8ad4925369.jpg' },
    'zh-Hant': { video: MEDIA + 'demo-zh-Hant-f3a541a44435.mp4', poster: MEDIA + 'poster-zh-Hant-98dc88b8abc4.jpg' },
  },
  // Portrait 1080×2338, 114.7 s and 155.3 s, with a full-width picture and top safe space.
  // Each cut retains 30 seconds of the call sheet before the smaller orb and bubble.
  phone: {
    'en': { video: MEDIA + 'demo-en-phone-21cfaf2e686a.mp4', poster: MEDIA + 'poster-en-phone-49c54d28003a.jpg' },
    'zh-Hant': { video: MEDIA + 'demo-zh-Hant-phone-c85af090b6cb.mp4', poster: MEDIA + 'poster-zh-Hant-phone-83893b529da2.jpg' },
  },
}

/**
 * The film for this screen and language: the phone cut on a narrow screen,
 * Chinese for a console shown in Chinese, and the desktop cut of the same
 * language where no phone cut exists yet.
 */
export function demoFilmFor(language: string, narrow: boolean): DemoFilm {
  const lang = /^zh/i.test(language) ? 'zh-Hant' : 'en'
  return (narrow ? DEMO_FILMS.phone[lang] : null) ?? DEMO_FILMS.desktop[lang]!
}
