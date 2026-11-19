/** Keep an exiting surface on screen until its own CSS transitions finish. */
export function afterSurfaceExit(surface:HTMLElement,finish:()=>void) {
  const animations=surface.getAnimations()
  if(!animations.length){finish();return}
  let current=true
  void Promise.allSettled(animations.map(animation=>animation.finished)).then(()=>{if(current)finish()})
  return ()=>{current=false}
}
