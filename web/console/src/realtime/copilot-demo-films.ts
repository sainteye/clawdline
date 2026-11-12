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
  // The phone cuts are being re-recorded; until they are published a phone plays the desktop cut.
  phone: { 'en': null, 'zh-Hant': null },
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
