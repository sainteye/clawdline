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
    // 1920×1080, 94.8 s and 97.3 s, published and checked by the R2 Session on 2026-11-12.
    'en': { video: MEDIA + 'demo-en-b206a48a82f1.mp4', poster: MEDIA + 'poster-en-5433ddf1654e.jpg' },
    'zh-Hant': { video: MEDIA + 'demo-zh-Hant-9eac800f8c01.mp4', poster: MEDIA + 'poster-zh-Hant-e3afa4a1f41f.jpg' },
  },
  // Portrait 1080×2338, 93.4 s and 96.2 s (about 19 MB each), re-recorded after the call sheet's
  // phone fix and published and checked by the R2 Session on 2026-11-12.
  phone: {
    'en': { video: MEDIA + 'demo-en-phone-878bbbb83cbe.mp4', poster: MEDIA + 'poster-en-phone-70e64a1f73ed.jpg' },
    'zh-Hant': { video: MEDIA + 'demo-zh-Hant-phone-3ee5cdfa57a9.mp4', poster: MEDIA + 'poster-zh-Hant-phone-d346e18373c0.jpg' },
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
