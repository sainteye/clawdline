/* Browser translation of the pinned World of Codecraft and Kaiju City renderers.
 * All raster art is vendored from the original authors. Native commands, hooks,
 * provider inference and terminal inspection are deliberately outside this port.
 */
(() => {
  'use strict';
  const $ = id => document.getElementById(id);
  const world = META.kind === 'world';
  const states = ['ready', 'working', 'holding', 'waiting_you', 'waiting_session', 'unknown', 'milestone_complete', 'work_complete'];
  const tones = ['#a7b7ce', '#73b875', '#c9ae7a', '#e66550', '#6caccb', '#9b959d', '#d6b767', '#80b9a1'];
  const copy = {
    en: {world: 'World of Codecraft', kaiju: 'Kaiju City', still: 'Still mode', list: 'Readable list', close: 'Close', light: 'Day / night',
      scope: 'All authorized machines · All Projects', detail: 'Selected Session', roster: 'Session register', choose: 'Select a character or a named Session.',
      search: 'Find a Session or Project', empty: 'No Sessions in the current reading.', fresh: 'Fresh reading', stale: 'May be stale', unknown: 'Freshness unknown',
      open: 'Open Session', required: 'A fresh reading is required to open.', noProject: 'No Project', disconnected: 'Disconnected · retained readings are stale',
      awaiting: 'Waiting for a fresh reading', loading: 'Preparing original artwork…', error: 'Could not open Session', timeout: 'Navigation outcome is uncertain. Check Clawdline before trying again.',
      worldHint: 'Drag the landscape · Use the map or arrow keys · Select a hero', kaijuHint: 'Select a monster · Use the arrows for more Projects',
      page: 'District', total: 'Sessions', needs: 'need you', working: 'working', licenses: 'Original licenses',
      worldNote: 'Heroes and classes are decorative assignments within this opening. Work, attention and reported completion follow Session states; they do not assert tool activity or acceptance.',
      kaijuNote: 'Project = building; Session = monster. Districts keep every Project reachable. Building wear uses a supplied working-turn time only; unknown duration stays intact. Ruins are a scene metaphor, not project damage or acceptance.',
      worldCredit: 'Original art and composition: Kevin Pardo · World of Codecraft · MIT. Cinzel Decorative: SIL OFL. Browser adaptation for Clawdline.',
      kaijuCredit: 'Original art and composition: Jiqize · Kaiju City · MIT. Browser adaptation for Clawdline.',
      reported: 'Completion is reported, not acceptance.', dismiss: 'Back to scene', members: 'Project Sessions', batch: 'On stage', previous: 'Previous monsters', next: 'Next monsters', idle: 'Idle Sessions stay in the register.', more: 'Select a Session',
      state: ['Ready', 'Working', 'Holding', 'Needs you', 'Waiting for Session', 'Unknown', 'Reported milestone complete', 'Reported work complete']},
    zh: {world: '奇幻村莊', kaiju: '怪獸城市', still: '靜態模式', list: '可讀清單', close: '關閉', light: '日景／夜景',
      scope: '所有已授權機器 · 所有 Project', detail: '選取的 Session', roster: 'Session 名冊', choose: '選取角色或具名的 Session。',
      search: '尋找 Session 或 Project', empty: '目前讀取中沒有 Session。', fresh: '最新讀取', stale: '可能過時', unknown: '新鮮度未知',
      open: '開啟 Session', required: '需要最新讀取才能開啟。', noProject: '未指定 Project', disconnected: '已斷線 · 保留的讀取資料可能過時',
      awaiting: '等待新的讀取資料', loading: '準備原作素材…', error: '無法開啟 Session', timeout: '導覽結果尚不確定，請先查看 Clawdline 再重試。',
      worldHint: '拖曳場景 · 小地圖或方向鍵移動 · 選取英雄', kaijuHint: '選取怪獸 · 使用箭頭查看其他 Project',
      page: '街區', total: '個 Session', needs: '需要你', working: '工作中', licenses: '原作授權聲明',
      worldNote: '英雄與職業是本次開啟中的裝飾分配。工作、等待與回報完成依 Session 狀態呈現，不代表實際工具活動或已驗收。',
      kaijuNote: 'Project 代表建築，Session 代表怪獸。切換街區可查看所有 Project。建築耗損僅依已提供的工作回合時間呈現，時長未知時維持完整；廢墟是場景比喻，不代表專案損毀或已驗收。',
      worldCredit: '原作美術與構圖：Kevin Pardo · World of Codecraft · MIT。Cinzel Decorative 字型採 SIL OFL。Clawdline 瀏覽器移植。',
      kaijuCredit: '原作美術與構圖：Jiqize · Kaiju City · MIT。Clawdline 瀏覽器移植。',
      reported: '完成是回報狀態，不代表已驗收。', dismiss: '返回場景', members: 'Project 的 Session', batch: '場上怪獸', previous: '上一批怪獸', next: '下一批怪獸', idle: '閒置 Session 留在名冊。', more: '選取 Session',
      state: ['就緒', '工作中', '暫停中', '需要你', '等待其他 Session', '未知', '回報里程碑完成', '回報工作完成']}
  };
  let context = {language: 'en', theme: 'dark', reducedMotion: true, visible: true, connection: 'disconnected'};
  let view, rows = [], stream = '', revision = -1, awaiting = true, selected = null, still = false, list = false, nightOverride = null;
  let busy = false, error = '', page = 0, raf = 0, last = 0, clock = 0, loaded = false, drag = null;
  const canvas = $('scene'), ctx = canvas.getContext('2d'), mini = $('minimap'), mctx = mini.getContext('2d');
  if(world)$('minimap-home').append(mini);
  const images = new Map(), units = new Map(), projects = new Map(), casts = new Map();
  let selectedProject, pickRefs = null, drawnCharacters = 0, markers = [];
  let width = 0, height = 0, zoom = innerWidth < 700 ? .85 : 1, camX = 0, camY = 0, cameraSet = false, hits = [], seenSnapshot = false;
  const t = () => context.language.startsWith('zh') ? copy.zh : copy.en;
  const state = r => states.includes(r.workState) ? r.workState : 'unknown';
  const tone = r => tones[states.indexOf(state(r))];
  const completed = r => ['milestone_complete', 'work_complete'].includes(state(r));
  const fresh = r => context.connection === 'connected' && !awaiting && r.freshness === 'fresh';
  const actionable = r => fresh(r) && state(r) !== 'unknown';
  const active = () => !still && !list && !context.reducedMotion && context.visible && context.connection === 'connected' && !awaiting;
  const moving = r => active() && actionable(r);
  const freshness = r => !fresh(r) ? (r.freshness === 'unknown' ? t().unknown : t().stale) : t().fresh;
  const projectName = r => r.project?.label ?? t().noProject;
  const projectKey = r => r.project?.ref ?? null;
  const night = () => nightOverride ?? (world ? new Date().getHours() < 6 || new Date().getHours() >= 19 : context.theme === 'dark');
  const hash = s => { let h = 2166136261; for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619); return h >>> 0; };
  const el = (name, text, cls) => { const e = document.createElement(name); if (text !== undefined) e.textContent = text; if (cls) e.className = cls; return e; };
  const image = (key, x, y, w, h, alpha = 1) => {
    const img = images.get(key); if (!img?.complete || !img.naturalWidth) return;
    ctx.save(); ctx.globalAlpha = alpha; ctx.drawImage(img, x, y, w, h); ctx.restore();
  };
  const heroNames = ['dwarf-artificer', 'elf-mage', 'human-warrior', 'orc-mage', 'undead-rogue', 'dwarf-priest'];
  // Exact original feet points, feet anchors, sprite timing and road from lieux.js/heros-data.js.
  const spots = [
    [[1010,300],[935,290],[1060,272],[820,296],[1090,312]],
    [[690,372],[640,342],[745,352],[470,338],[600,390]],
    [[2600,338],[2470,345],[2700,318],[2380,352],[2530,368],[2660,366],[2750,345],[2430,380]],
    [[690,372],[640,342],[745,352],[470,338],[600,390]],
    [[2020,395],[1735,320],[1040,318],[960,316],[1190,392]],
    [[2190,372],[2235,338],[2110,388],[2290,362],[2160,330]]
  ];
  const rest = [[1555,362],[1640,372],[1600,330],[1530,342],[1680,350],[1570,388],[1628,394],[1665,328],[1505,372],[1712,380]];
  const road = [[0,330],[200,320],[420,372],[640,345],[900,330],[1060,360],[1350,380],[1500,372],[1610,392],[1720,360],[1900,350],[2150,372],[2330,340],[2560,330],[2700,300]];
  function route(a, b) {
    if (Math.hypot(a[0]-b[0], a[1]-b[1]) < 180) return [a,b];
    const nearest = p => road.reduce((best, q, i) => Math.abs(q[0]-p[0]) < Math.abs(road[best][0]-p[0]) ? i : best, 0);
    const from = nearest(a), to = nearest(b), middle = road.slice(Math.min(from,to),Math.max(from,to)+1);
    return [a,...(from>to ? middle.reverse() : middle),b];
  }
  function groups() {
    const gs = new Map();
    for (const r of rows) { const key = projectKey(r); if (!gs.has(key)) gs.set(key,{key,label:projectName(r),rows:[]}); gs.get(key).rows.push(r); }
    return [...gs.values()];
  }
  // Match the source: idle agents stay in the roster; original stand points bound each cast.
  const onStage = r => state(r)==='working'||state(r)==='waiting_you'||completed(r);
  function cast(g,slot){
    const active=g.rows.filter(onStage),size=slot.stand.length;
    const batch=Math.min(casts.get(g.key)||0,Math.max(0,Math.ceil(active.length/size)-1));
    casts.set(g.key,batch);return{active,batch,size,shown:active.slice(batch*size,(batch+1)*size)};
  }
  function presentSelection(){
    if(!matchMedia('(max-width:700px)').matches&&!pickRefs)return;
    $('selection-content').append($('detail'));
    $('selection-title').textContent=t().detail;
    if(!$('selection').open)$('selection').showModal();
    $('selection').scrollTop=0;
  }
  function projectSelect(key){selectedProject=key;selected=null;pickRefs=null;renderUI();requestDraw();presentSelection();}
  function candidates(refs){pickRefs=refs;selected=null;renderUI();requestDraw();presentSelection();}
  function syncUnits() {
    const uses = new Map(), gs = groups();
    page = Math.min(page, Math.max(0,Math.ceil(gs.length / META.city?.slots.length)-1) || 0);
    const live=world?rows:gs.flatMap((g,i)=>cast(g,META.city.slots[i%META.city.slots.length]).shown);
    live.forEach((r, i) => {
      let u = units.get(r.ref);
      const avatar = u?.avatar ?? hash(r.ref) % (world ? heroNames.length : META.city.monsters.length);
      let target, depth = 0;
      if (world) {
        const resting = ['ready','holding'].includes(state(r)), pool = resting ? rest : spots[avatar], group = resting ? 'rest' : avatar;
        const n = uses.get(group) ?? 0; uses.set(group,n+1);
        const p = pool[n % pool.length];
        target = [p[0] + Math.floor(n/pool.length)*18,p[1] - Math.floor(n/pool.length)*3];
      } else {
        const projectIndex = gs.findIndex(g=>g.key===projectKey(r)), slot = META.city.slots[projectIndex % META.city.slots.length];
        const n = uses.get(projectIndex) ?? 0; uses.set(projectIndex,n+1);
        const p = slot.stand[n % slot.stand.length];
        target = [p.x,p.y]; depth = p.depth;
      }
      if (!u) {
        u={avatar,x:target[0],y:target[1],target,path:null,walking:false};
        if(!world&&seenSnapshot&&moving(r)){
          u.x=META.city.entry.x;u.y=META.city.entry.y;u.path=[[u.x,u.y],target];u.segment=1;u.walking=true;
        }
        units.set(r.ref,u);
      }
      else if (u.target[0]!==target[0] || u.target[1]!==target[1]) {
        const path = world ? route([u.x,u.y],target) : [[u.x,u.y],target];
        if (moving(r)) { u.path=path; u.segment=1; u.walking=true; }
        else { u.x=target[0];u.y=target[1];u.path=null;u.walking=false; }
      }
      u.target=target; u.r=r; u.depth=depth; u.order=i;
      // Unknown, stale and paused data cannot keep a character walking.
      if (!moving(r)) {u.x=target[0];u.y=target[1];u.path=null;u.walking=false;}
    });
    for (const ref of units.keys()) if (!live.some(r=>r.ref===ref)) units.delete(ref);
    for (const key of projects.keys()) if (!gs.some(g=>g.key===key)) projects.delete(key);
    for(const key of casts.keys())if(!gs.some(g=>g.key===key))casts.delete(key);
  }
  function updatePositions(dt) {
    for (const u of units.values()) {
      if (!u.path || !moving(u.r)) continue;
      const p=u.path[u.segment],dx=p[0]-u.x,dy=p[1]-u.y,d=Math.hypot(dx,dy);
      const speed=world ? META.heroes[heroNames[u.avatar]].speed : Math.hypot(u.target[0]-u.path[0][0],u.target[1]-u.path[0][1])/.9;
      const step=speed*dt/1000;
      u.direction=dy<0?(dx<0?'ul':'ur'):(dx<0?'dl':'dr');
      if(d<=step){u.x=p[0];u.y=p[1];u.segment++;if(u.segment===u.path.length){u.path=null;u.walking=false;}}
      else {u.x+=dx/d*step;u.y+=dy/d*step;}
    }
  }
  function resize() {
    const box=canvas.getBoundingClientRect(),ratio=devicePixelRatio||1;
    width=box.width;height=box.height;document.documentElement.style.setProperty('--header-height',document.querySelector('header').getBoundingClientRect().height+'px');
    canvas.width=Math.round(width*ratio);canvas.height=Math.round(height*ratio);
    mini.width=Math.round(mini.clientWidth*ratio);mini.height=Math.round(mini.clientHeight*ratio);
    if(world&&!cameraSet){camX=1500-width/zoom/2;camY=(400-height/zoom)/2;cameraSet=true;}
    clampCamera();requestDraw();
  }
  function clampCamera(){if(!world)return;camX=Math.max(0,Math.min(Math.max(0,3000-width/zoom),camX));camY=Math.max(-400,Math.min(800-height/zoom,camY));}
  function choose(ref, follow=true) {
    selected=ref;pickRefs=null;const r=rows.find(r=>r.ref===ref);selectedProject=r?projectKey(r):undefined;
    if(!world&&r){const g=groups().find(g=>g.key===projectKey(r)),slot=META.city.slots[groups().findIndex(item=>item.key===g.key)%META.city.slots.length],at=g.rows.filter(onStage).findIndex(r=>r.ref===ref);if(at>=0)casts.set(g.key,Math.floor(at/slot.stand.length));syncUnits();}
    const u=units.get(ref);
    if(u&&follow&&world){camX=u.x-width/zoom/2;camY=u.y-height/zoom*.72;clampCamera();}
    if(r&&follow&&!world)page=Math.floor(groups().findIndex(g=>g.key===projectKey(r))/META.city.slots.length);
    renderUI();requestDraw();presentSelection();
  }
  function drawHero(u) {
    const r=u.r,hero=META.heroes[heroNames[u.avatar]],playing=moving(r);
    let pose=u.walking&&playing?'walk_'+u.direction:state(r)==='working'?'work':['ready','holding'].includes(state(r))?'rest':'idle';
    const info=hero.anims[pose],n=playing?Math.floor(clock*info.fps/1000)%info.n:0;
    const key='art/heros/'+heroNames[u.avatar]+'/'+pose+'/'+String(n).padStart(2,'0')+(night()?'-n':'')+'.webp';
    const alpha=actionable(r)?1:.42;
    const ring=selected===r.ref?'ring-selected':state(r)==='waiting_you'?'ring-blocked':completed(r)?'ring-done':null;
    if(ring)image('art/monde/'+ring+'.webp',u.x-30,u.y-17,60,33,alpha*(playing&&selected!==r.ref?.65+.35*Math.sin(clock/300):1));
    image(key,u.x-info.ax,u.y-info.ay,info.w,info.h,alpha);
    if(state(r)==='waiting_you'||completed(r)){
      const mark=state(r)==='waiting_you'?'m-blocked':'m-done',w=mark==='m-blocked'?24:28,h=38;
      markers.push({r,key:'art/monde/'+mark+'.webp',x:(u.x-camX)*zoom,y:(u.y-info.ay-h-8-camY)*zoom});
    }
    hits.push({x:u.x-24,y:u.y-info.ay,w:48,h:info.ay+14,ref:r.ref});
    drawnCharacters++;
  }
  function renderAnnotations(){
    const layer=$('annotations'),old=new Map([...layer.children].map(e=>[e.dataset.key,e])),placed=[];
    const label=$('scene-label'),r=rows.find(r=>r.ref===selected),u=units.get(selected);
    label.hidden=!world||!r||!u;
    if(!label.hidden){if(label.textContent!==r.title)label.replaceChildren(el('span',r.title));label.setAttribute('aria-label',r.title+' · '+t().more);label.onclick=()=>{renderUI();presentSelection();};}
    const obstacles=[];
    const viewport=canvas.parentElement.getBoundingClientRect();
    for(const node of [...document.querySelectorAll('.hud span:not([hidden])'),...(!label.hidden?[label]:[])]){const b=node.getBoundingClientRect();obstacles.push({x:b.x-viewport.x,y:b.y-viewport.y,w:b.width,h:b.height});}
    const overlaps=(a,b)=>a.x<b.x+b.w&&a.x+a.w>b.x&&a.y<b.y+b.h&&a.y+a.h>b.y;
    for(const m of markers.sort((a,b)=>(b.r.ref===selected)-(a.r.ref===selected))){
      if(m.x<0||m.x>width||m.y+44<0||m.y>height)continue;
      const rect={x:Math.max(4,Math.min(width-68,m.x-32)),y:Math.max(4,m.y),w:64,h:44};
      while(rect.y+rect.h<=height-4&&[...obstacles,...placed].some(b=>overlaps(rect,b)))rect.y+=rect.h+4;
      if(rect.y+rect.h>height-4){const nearest=placed.reduce((a,b)=>!a||Math.hypot(b.x-m.x,b.y-m.y)<Math.hypot(a.x-m.x,a.y-m.y)?b:a,null);if(nearest)nearest.refs.push(m.r.ref);continue;}
      placed.push({...rect,key:m.r.ref,refs:[m.r.ref],image:m.key});
    }
    for(const item of placed){
      const button=old.get(item.key)||el('button',undefined,'status-marker');old.delete(item.key);button.dataset.key=item.key;button.classList.toggle('selected',item.refs.includes(selected));button.style.left=item.x+'px';button.style.top=item.y+'px';
      if(Math.abs(item.y-markers.find(m=>m.r.ref===item.key).y)>4){const m=markers.find(m=>m.r.ref===item.key);ctx.strokeStyle='#e1c18399';ctx.beginPath();ctx.moveTo(m.x,m.y+38);ctx.lineTo(item.x+32,item.y+22);ctx.stroke();}
      const refs=item.refs,signature=refs.join('|')+'|'+item.image;
      if(button.dataset.signature!==signature){button.dataset.signature=signature;button.replaceChildren();const img=el('img');img.src=ART[item.image];img.alt='';button.append(img);if(refs.length>1)button.append(el('span','+'+(refs.length-1)));}
      button.setAttribute('aria-label',refs.map(ref=>rows.find(r=>r.ref===ref)?.title).join(' · '));button.onclick=()=>refs.length===1?choose(refs[0],false):candidates(refs);if(button.parentElement!==layer)layer.append(button);
    }
    for(const button of old.values())button.remove();
  }
  function drawWorld(){
    ctx.save();ctx.scale(zoom,zoom);ctx.translate(-camX,-camY);ctx.imageSmoothingEnabled=true;
    const mode=night()?'nuit':'jour';
    for(let k=0;k<3;k++){
      image('art/monde/nord-'+k+'-'+mode+'.webp',k*1000,-400,1000,400);
      image('art/monde/'+mode+'-'+k+'.webp',k*1000,0,1000,400);
      image('art/monde/sud-'+k+'-'+mode+'.webp',k*1000,400,1000,400);
    }
    const ordered=[...units.values()].sort((a,b)=>a.y-b.y);
    // Reapply the original transparent foreground cut-outs over units behind them.
    for(const u of ordered){
      drawHero(u);
      for(const f of META.fronts)if(u.y+3<f.z&&u.x+60>=f.x&&u.x-60<=f.x+f.w)
        image('art/monde/avant-'+f.key+'-'+mode+'.webp',f.x,f.y,f.w,f.h);
    }
    if(active())for(const [sx,sy] of [[827,95],[1590,47],[1639,100]])for(let k=0;k<3;k++){
      const p=((clock+k*520)%(2800+k*520))/(2800+k*520);ctx.fillStyle=`rgba(220,216,225,${.65*(1-p)})`;
      ctx.beginPath();ctx.arc(sx-4+p*(10+k*6),sy-8-p*(44+k*10),4+k,0,Math.PI*2);ctx.fill();
    }
    for(const [x,y,size,color,period] of [[1598,345,80,'255,161,43',700],[815,236,50,'255,138,43',650],[558,160,60,'168,107,255',2200]]){
      if(!night()&&x!==1598)continue;
      const a=active()?.08+.05*Math.sin(clock/period*Math.PI*2):.09;
      const gradient=ctx.createRadialGradient(x,y,0,x,y,size/2);gradient.addColorStop(0,`rgba(${color},${a})`);gradient.addColorStop(1,`rgba(${color},0)`);
      ctx.fillStyle=gradient;ctx.fillRect(x-size/2,y-size/2,size,size);
    }
    ctx.restore();drawMinimap();
  }
  function drawMinimap(){
    const ratio=devicePixelRatio||1,w=mini.clientWidth,h=mini.clientHeight;mctx.setTransform(ratio,0,0,ratio,0,0);mctx.clearRect(0,0,w,h);
    const img=images.get('art/monde/minicarte.webp');if(img?.complete)mctx.drawImage(img,0,0,w,h);
    for(const u of units.values()){mctx.fillStyle=tone(u.r);mctx.fillRect(u.x/3000*w-2,u.y/400*h-2,4,4);}
    mctx.strokeStyle='#fff3c4';mctx.lineWidth=2;mctx.strokeRect(camX/3000*w,1,width/zoom/3000*w,h-2);
  }
  function damage(g){
    let prior=projects.get(g.key)??{level:0,next:0};
    if(!g.rows.every(fresh))return prior.level;
    let level=0;
    if(g.rows.some(completed))level=3;
    else if(g.rows.some(r=>state(r)==='working')){
      const times=g.rows.filter(r=>state(r)==='working'&&r.stateTime?.basis==='turn'&&Number.isFinite(r.stateTime.at)&&r.stateTime.at<=Date.now()).map(r=>r.stateTime.at);
      const age=times.length?Date.now()-Math.min(...times):0;level=age>=8*60000?2:age>=2*60000?1:0;
    }else if(g.rows.some(r=>state(r)==='waiting_you'))level=prior.level;
    // Source repair cadence, measured from an observed state change in this opening.
    if(level<prior.level&&active()){
      if(!prior.next)prior.next=clock+650;
      if(clock>=prior.next){prior.level--;prior.next=clock+650;}
    }else prior={level,next:0};
    projects.set(g.key,prior);return prior.level;
  }
  function drawKaiju(){
    const M=META.city,theme=night()?'night':'day',base='assets/'+theme+'/',gs=groups(),visible=gs.slice(page*M.slots.length,(page+1)*M.slots.length);
    const k=Math.min(width/M.canvas.w,height/M.canvas.h),ox=(width-M.canvas.w*k)/2,oy=(height-M.canvas.h*k)/2;
    ctx.save();ctx.translate(ox,oy);ctx.scale(k,k);ctx.imageSmoothingEnabled=false;
    image(base+'plate.webp',0,0,M.canvas.w,M.canvas.h);
    const ordered=[];
    for(const slot of M.slots){const g=visible[slot.i];ordered.push({depth:slot.depth,slot,g});}
    for(const u of units.values())if(visible.some(g=>g.key===projectKey(u.r)))ordered.push({depth:u.walking?Infinity:u.depth,u});
    ordered.sort((a,b)=>a.depth-b.depth||(a.u?.y??0)-(b.u?.y??0));
    for(const item of ordered){
      if(item.slot){
        const s=item.slot,g=item.g,level=g?damage(g):0,smashing=g?.rows.some(r=>state(r)==='working'&&moving(r));
        image(base+(g?s.layers[level]:s.empty).replace('.png','.webp'),smashing?Math.sin(clock/240*Math.PI*2):0,0,M.canvas.w,M.canvas.h);
        if(!g)continue;
        hits.push({x:ox+s.hit.x*k,y:oy+s.hit.y*k,w:s.hit.w*k,h:s.hit.h*k,project:g.key});
        if(smashing){const [w,h]=M.fxSize.dust;image(base+M.fx.dust.replace('.png','.webp'),s.hit.x+s.hit.w/2-w/2,s.hit.y+s.hit.h-h-2,w,h,.55+.25*Math.sin(clock/700));}
        if(g.rows.some(r=>state(r)==='waiting_you')){const [w,h]=M.fxSize.siren;image(base+M.fx.siren.replace('.png','.webp'),s.roof.x-w/2,s.roofs[level]-h-1,w,h,g.rows.some(moving)?.6+.4*Math.sin(clock/500):1);}
      }else{
        const u=item.u,r=u.r,m=M.monsters[u.avatar],playing=moving(r),s=state(r);drawnCharacters++;
        const frame=u.walking&&playing?'walk':s==='working'?'smash':s==='waiting_you'?'wave':completed(r)?'roar':'idle';
        const shake=playing&&frame==='smash'?Math.sin(clock/180*Math.PI*2):0;
        image(base+m.frames[frame].replace('.png','.webp'),u.x-M.sprite.ax+shake,u.y-M.sprite.ay,M.sprite.w,M.sprite.h,actionable(r)?1:.42);
        const bubble=s==='waiting_you'?'question':completed(r)?'bang':null;
        if(bubble){const [w,h]=M.fxSize[bubble];image(base+M.fx[bubble].replace('.png','.webp'),u.x+14,u.y-M.sprite.ay+m.top-h-2,w,h);}
        hits.push({x:ox+(u.x-22)*k,y:oy+(u.y-M.sprite.ay)*k,w:44*k,h:M.sprite.ay*k,ref:r.ref});
        if(selected===r.ref){ctx.strokeStyle=night()?'#ffd877':'#8f4c28';ctx.lineWidth=1.5;ctx.beginPath();ctx.ellipse(u.x,u.y-2,22,7,0,0,Math.PI*2);ctx.stroke();}
      }
    }
    ctx.restore();
    const layer=$('project-strip'),old=new Map([...layer.children].map(e=>[e.dataset.key,e]));
    visible.forEach((g,i)=>{
      const slot=M.slots[i],c=cast(g,slot),key=String(g.key),b=old.get(key)||el('button',undefined,'project-marker');old.delete(key);b.dataset.key=key;const caption=c.active.length?((c.batch*c.size+1)+'–'+Math.min(c.active.length,(c.batch+1)*c.size)+' / '+c.active.length):t().idle,signature=g.label+'|'+caption;if(b.dataset.signature!==signature){b.dataset.signature=signature;b.replaceChildren(el('b',g.label),el('small',caption));}
      b.setAttribute('aria-pressed',String(g.key===selectedProject));b.onclick=()=>projectSelect(g.key);if(b.parentElement!==layer)layer.append(b);
    });
    for(const b of old.values())b.remove();
  }
  function needsAnimation(){return active()&&rows.some(actionable);}
  function draw(now){
    raf=0;
    // Match the original shared 10 fps sprite timer, rather than repainting at display refresh rate.
    if(needsAnimation()&&last&&now-last<100){raf=requestAnimationFrame(draw);return;}
    const dt=last?Math.min(now-last,100):0;last=now;
    if(active())clock+=dt;
    updatePositions(dt);hits=[];markers=[];drawnCharacters=0;
    ctx.setTransform(devicePixelRatio||1,0,0,devicePixelRatio||1,0,0);ctx.clearRect(0,0,width,height);
    if(world){drawWorld();renderAnnotations();}else drawKaiju();
    canvas.dataset.characters=String(drawnCharacters);canvas.dataset.phase=String(Math.round(clock));
    if(needsAnimation())raf=requestAnimationFrame(draw);
  }
  function requestDraw(){if(!raf&&context.visible&&!list)raf=requestAnimationFrame(draw);}
  function pause(){if(raf)cancelAnimationFrame(raf);raf=0;last=0;syncUnits();requestDraw();}
  function renderUI(){
    const w=t();document.documentElement.lang=context.language.startsWith('zh')?'zh-Hant-TW':'en';document.title=world?w.world:w.kaiju;
    document.body.className=(world?'codecraft':'kaiju')+(night()?' night':'')+(list?' list-mode':'');
    $('title').textContent=world?w.world:w.kaiju;$('scope').textContent=context.scopeLabel||w.scope;
    for(const id of ['still','list','close'])$(id).textContent=w[id];$('lighting').textContent=w.light;
    $('still').setAttribute('aria-pressed',String(still));$('list').setAttribute('aria-pressed',String(list));
    canvas.setAttribute('aria-label',(world?w.world:w.kaiju)+' · '+(world?w.worldHint:w.kaijuHint));
    mini.setAttribute('aria-label',w.worldHint);$('hint').textContent=world?w.worldHint:w.kaijuHint;
    $('dismiss-selection').textContent=w.dismiss;$('selection-title').textContent=w.detail;
    $('detail-title').textContent=w.detail;$('roster-title').textContent=w.roster;$('search').placeholder=w.search;$('search').setAttribute('aria-label',w.search);
    $('empty').textContent=w.empty;$('empty').hidden=rows.length!==0||!loaded;
    $('counts').textContent=rows.length+' '+w.total+' · '+rows.filter(r=>state(r)==='working').length+' '+w.working+' · '+rows.filter(r=>state(r)==='waiting_you').length+' '+w.needs;
    const message=context.connection!=='connected'?w.disconnected:awaiting?w.awaiting:!loaded?w.loading:'';
    $('warning').textContent=message;$('warning').hidden=!message;
    $('adaptation').textContent=world?w.worldNote:w.kaijuNote;$('credit').textContent=world?w.worldCredit:w.kaijuCredit;$('licenses-title').textContent=w.licenses;
    $('legend').replaceChildren(...states.map((s,i)=>{const n=el('span',w.state[i]);n.style.setProperty('--tone',tones[i]);return n;}));
    const query=$('search').value.toLocaleLowerCase(),shown=rows.filter(r=>(r.title+' '+projectName(r)).toLocaleLowerCase().includes(query));
    const activeElement=document.activeElement?.dataset.ref;
    const scrollTop=$('register').scrollTop;
    $('register').replaceChildren(...shown.map(r=>{
      const b=el('button',undefined,'row'+(selected===r.ref?' selected':''));b.dataset.ref=r.ref;b.setAttribute('aria-pressed',String(selected===r.ref));b.style.setProperty('--tone',tone(r));
      const text=el('span');text.append(el('span',r.title,'name'),el('span',projectName(r)+' · '+w.state[states.indexOf(state(r))]+' · '+freshness(r),'meta'));
      b.append(el('span',undefined,'dot'),text);b.onclick=()=>choose(r.ref);return b;
    }));
    $('register').scrollTop=scrollTop;
    $('roster-count').textContent=shown.length===rows.length?String(rows.length):shown.length+' / '+rows.length;
    if(activeElement)[...$('register').children].find(e=>e.dataset.ref===activeElement)?.focus({preventScroll:true});
    const r=rows.find(r=>r.ref===selected);$('detail').replaceChildren();
    if(r){
      $('detail').append(el('p',projectName(r)),el('h2',r.title),el('p',w.state[states.indexOf(state(r))]+' · '+freshness(r)));
      if(completed(r))$('detail').append(el('p',w.reported));
      if(!actionable(r))$('detail').append(el('p',w.required));
      const open=el('button',w.open);open.id='open-session';open.disabled=busy||!actionable(r);
      open.onclick=async()=>{if(busy||!actionable(r))return;busy=true;renderUI();try{await view.sessions.open(r.ref);}catch(e){error=e.code==='request_timeout'?t().timeout:t().error+' · '+(e.code||'host_unavailable');}finally{busy=false;renderUI();}};
      $('detail').append(open);
    }else $('detail').append(el('p',w.choose));
    const group=!world?groups().find(g=>g.key===selectedProject):null;
    const members=pickRefs?rows.filter(r=>pickRefs.includes(r.ref)):group?.rows;
    if(members){
      if(group){
        const slot=META.city.slots[groups().findIndex(item=>item.key===group.key)%META.city.slots.length],c=cast(group,slot),controls=el('div',undefined,'cast-controls');
        controls.append(el('p',group.label+' · '+w.batch+' '+(c.active.length?((c.batch*c.size+1)+'–'+Math.min(c.active.length,(c.batch+1)*c.size)):'0')+' / '+c.active.length));
        for(const [direction,title] of [[-1,w.previous],[1,w.next]]){const button=el('button',title);button.disabled=direction<0?c.batch===0:(c.batch+1)*c.size>=c.active.length;button.onclick=()=>{casts.set(group.key,c.batch+direction);syncUnits();renderUI();requestDraw();};controls.append(button);}
        $('detail').append(controls);
      }
      $('detail').append(el('p',w.members,'member-heading'));
      for(const member of members){const button=el('button',member.title+' · '+w.state[states.indexOf(state(member))],'project-member');button.setAttribute('aria-pressed',String(selected===member.ref));button.onclick=()=>choose(member.ref);$('detail').append(button);}
    }
    $('error').textContent=error;
    if(!world){const gs=groups(),pages=Math.max(1,Math.ceil(gs.length/META.city.slots.length));$('district-label').textContent=w.page+' '+(page+1)+' / '+pages;$('prev').disabled=page===0;$('next').disabled=page===pages-1;}
  }
  function snapshot(data){
    if(data.streamId===stream&&data.revision<=revision)return;
    if(data.streamId!==stream){units.clear();projects.clear();seenSnapshot=false;}
    stream=data.streamId;revision=data.revision;rows=[...data.sessions];awaiting=context.connection!=='connected';
    if(selected&&!rows.some(r=>r.ref===selected))selected=null;
    if(pickRefs)pickRefs=pickRefs.filter(ref=>rows.some(r=>r.ref===ref));
    if(!rows.length&&$('selection').open)$('selection').close();
    syncUnits();seenSnapshot=true;renderUI();requestDraw();
  }
  function point(e){const b=canvas.getBoundingClientRect();return [e.clientX-b.left,e.clientY-b.top];}
  canvas.onpointerdown=e=>{if(!world)return;const p=point(e);drag={id:e.pointerId,x:p[0],y:p[1],camX,camY,moved:false};canvas.setPointerCapture(e.pointerId);canvas.classList.add('dragging');};
  canvas.onpointermove=e=>{if(!drag||e.pointerId!==drag.id)return;const p=point(e);if(Math.hypot(p[0]-drag.x,p[1]-drag.y)>5)drag.moved=true;if(drag.moved){camX=drag.camX-(p[0]-drag.x)/zoom;camY=drag.camY-(p[1]-drag.y)/zoom;clampCamera();requestDraw();}};
  canvas.onpointerup=e=>{const p=point(e),moved=drag?.moved;drag=null;canvas.classList.remove('dragging');if(moved)return;const [x,y]=world?[p[0]/zoom+camX,p[1]/zoom+camY]:p;const hit=[...hits].reverse().find(h=>x>=h.x&&x<=h.x+h.w&&y>=h.y&&y<=h.y+h.h);if(hit){if('project' in hit)projectSelect(hit.project);else choose(hit.ref,false);}};
  canvas.onpointercancel=()=>{drag=null;canvas.classList.remove('dragging');};
  canvas.onkeydown=e=>{if(!world)return;const dx=e.key==='ArrowLeft'?-100:e.key==='ArrowRight'?100:0,dy=e.key==='ArrowUp'?-100:e.key==='ArrowDown'?100:0;if(dx||dy){e.preventDefault();camX+=dx/zoom;camY+=dy/zoom;clampCamera();requestDraw();}};
  mini.onpointerdown=e=>{const b=mini.getBoundingClientRect();camX=(e.clientX-b.left)/b.width*3000-width/zoom/2;clampCamera();requestDraw();};
  mini.onkeydown=e=>{if(e.key==='ArrowLeft'||e.key==='ArrowRight'){e.preventDefault();camX+=e.key==='ArrowLeft'?-200:200;clampCamera();requestDraw();}};
  $('zoomIn').onclick=()=>{zoom*=1.2;clampCamera();requestDraw();};$('zoomOut').onclick=()=>{zoom=Math.max(width/3000,zoom/1.2);clampCamera();requestDraw();};
  $('still').onclick=()=>{still=!still;pause();renderUI();};$('list').onclick=()=>{list=!list;renderUI();resize();pause();};
  $('lighting').onclick=()=>{nightOverride=!night();renderUI();requestDraw();};$('close').onclick=()=>view?.close().catch(e=>{error=e.code;renderUI();});
  $('dismiss-selection').onclick=()=>$('selection').close();
  $('selection').addEventListener('close',()=>{$('detail-home').append($('detail'));});
  $('search').oninput=renderUI;
  $('prev').onclick=()=>{page--;renderUI();requestDraw();};$('next').onclick=()=>{page++;renderUI();requestDraw();};
  document.addEventListener('keydown',e=>{if(e.key==='Escape'){if($('selection').open)return;e.preventDefault();if(selected){selected=null;renderUI();requestDraw();}else view?.close().catch(()=>{});}});
  const observer=new ResizeObserver(resize);observer.observe(canvas.parentElement);observer.observe(document.querySelector('header'));
  renderUI();resize();
  Promise.all(Object.entries(ART).map(([key,src])=>new Promise((resolve,reject)=>{const img=new Image();images.set(key,img);img.onload=resolve;img.onerror=()=>reject(Error('original_art_unavailable'));img.src=src;}))).then(()=>{loaded=true;document.body.dataset.artReady='true';renderUI();requestDraw();}).catch(e=>{error=e.message;renderUI();});
  connectViewPlugin().then(async api=>{view=api;view.onContext(next=>{if(next.connection!=='connected')awaiting=true;context=next;pause();renderUI();requestDraw();});snapshot(await view.sessions.subscribe(snapshot));}).catch(e=>{error=t().error+' · '+(e.code||'host_unavailable');renderUI();});
})();
