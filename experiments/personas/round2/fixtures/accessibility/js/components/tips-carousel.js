const INTERVAL_MS = 5000;

export function startTipsCarousel(track) {
  if (!track) return () => {};

  const slides = Array.from(track.children);
  if (slides.length < 2) return () => {};

  let index = 0;

  function show(next) {
    index = (next + slides.length) % slides.length;
    track.style.transform = `translateX(-${index * 100}%)`;
    slides.forEach((slide, i) => {
      slide.setAttribute('aria-hidden', String(i !== index));
      for (const link of slide.querySelectorAll('a')) {
        link.tabIndex = i === index ? 0 : -1;
      }
    });
  }

  show(0);
  const timer = window.setInterval(() => show(index + 1), INTERVAL_MS);

  return () => window.clearInterval(timer);
}
