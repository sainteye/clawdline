/* Kaiju's roster and news, translated from the pinned roster.js/news.js.
 * Identity is decorative and lasts for this opening. No native statistics are inferred.
 */
function createKaijuUI({read, $, el, t, state, fresh, actionable, projectName, projectKey, choose, projectSelect, renderCast, art, meta}) {
  const words = {
    en: {roster:'Monster roster', files:'Dossier cards', avatars:'Avatar tiles', file:'KAIJU FILE', code:'Code', task:'Task', place:'District', condition:'Status', damage:'Wear', time:'Time', unknownTime:'Not supplied', callback:'Waiting for callback', waiting:'Needs you', completed:'Completion reported', news:'Monster news', clear:'Clear news', more:'{count} more · available in the roster', age:'{minutes} min', damageNames:['Intact','Light wear','Heavy wear','Ruins'], outside:'Off stage', monster:['Lizard','Gorilla','Crab','Axolotl','Yeti','Moth','Turtle','Bat']},
    zh: {roster:'怪獸名冊', files:'檔案卡', avatars:'頭像方塊', file:'怪獸檔案', code:'代號', task:'任務', place:'駐地', condition:'狀態', damage:'耗損', time:'時間', unknownTime:'未提供', callback:'等待 callback', waiting:'需要你', completed:'回報完成', news:'怪獸快訊', clear:'清除快訊', more:'另有 {count} 則，可由名冊查看', age:'{minutes} 分鐘', damageNames:['完好','輕損','重損','廢墟'], outside:'場外', monster:['巨蜥','巨猿','巨蟹','美西螈','雪怪','巨蛾','巨龜','巨蝠']}
  };
  const assignments=new Map(), wear=new Map();
  let next=0, mode='files', last=new Map(), items=[], minute=-1, newsSignature='';
  const newsPainted=new Set();
  const w=()=>read().context.language.startsWith('zh')?words.zh:words.en;
  const callback=r=>r.waitKind==='callback'&&!['working','waiting_you','milestone_complete','work_complete'].includes(state(r));
  const status=r=>callback(r)?w().callback:t().state[['ready','working','holding','waiting_you','waiting_session','unknown','milestone_complete','work_complete'].indexOf(state(r))];
  const pose=r=>state(r)==='working'?'smash':state(r)==='waiting_you'?'wave':['milestone_complete','work_complete'].includes(state(r))?'roar':'idle';
  const hot=r=>state(r)==='waiting_you'||['milestone_complete','work_complete'].includes(state(r));
  const asset=path=>art['assets/'+(read().night?'night':'day')+'/'+path.replace('.png','.webp')];
  const assignment=ref=>assignments.get(ref)??0;
  const monster=r=>meta.city.monsters[assignment(r.ref)];
  const freshness=r=>fresh(r)?t().fresh:r.freshness==='unknown'?t().unknown:t().stale;
  function reset(){assignments.clear();wear.clear();next=0;last.clear();items=[];newsPainted.clear();newsSignature='';}
  function sync(){
    const {rows,selected,context,awaiting}=read(), live=new Set(rows.map(r=>r.ref));
    for(const r of rows)if(!assignments.has(r.ref)){assignments.set(r.ref,next);next=(next+1)%meta.city.monsters.length;}
    for(const ref of assignments.keys())if(!live.has(ref))assignments.delete(ref);
    for(const ref of newsPainted)if(!live.has(ref))newsPainted.delete(ref);
    for(const key of wear.keys())if(!rows.some(r=>projectKey(r)===key))wear.delete(key);
    if(context.connection!=='connected'||awaiting){last.clear();items=[];return;}
    for(const r of rows){
      const previous=last.get(r.ref);
      if(previous&&fresh(previous)&&fresh(r)&&!hot(previous)&&hot(r)&&!r.focused&&r.ref!==selected){
        items=items.filter(ref=>ref!==r.ref);items.unshift(r.ref);newsPainted.delete(r.ref);
      }
    }
    last=new Map(rows.map(r=>[r.ref,r]));
    items=items.filter(ref=>rows.some(r=>r.ref===ref&&fresh(r)&&hot(r)&&!r.focused&&ref!==selected));
  }
  function portrait(r,cls='kaiju-photo'){
    const m=monster(r),box=el('span',undefined,cls),img=el('img');img.src=asset(m.avatars[pose(r)]);img.alt=w().monster[assignment(r.ref)];img.width=48;img.height=48;
    box.append(img);
    const clip=el('img',undefined,'kaiju-clip');clip.src=asset(meta.city.fx.clip);clip.alt='';box.append(clip);
    return box;
  }
  function time(r){
    const time=r.stateTime;
    if(!time||!fresh(r)||callback(r)||!['working','waiting_you','milestone_complete','work_complete'].includes(state(r))||!Number.isFinite(time.at)||time.at>Date.now()||(state(r)==='working'&&time.basis!=='turn'))return null;
    const minutes=Math.max(0,Math.floor((Date.now()-time.at)/60000)),zh=read().context.language.startsWith('zh');
    const duration=minutes<60?w().age.replace('{minutes}',String(minutes)):zh?Math.floor(minutes/60)+' 小時 '+minutes%60+' 分鐘':Math.floor(minutes/60)+' h '+minutes%60+' min';
    if(state(r)==='working')return minutes?(zh?'拆了 ':'Smashing for ')+duration:(zh?'剛開拆':'Just started smashing');
    if(state(r)==='waiting_you')return minutes?(zh?'等了 ':'Waiting for ')+duration:(zh?'剛開始等':'Just started waiting');
    return minutes?duration+(zh?'前回報完成':' since reported completion'):(zh?'剛回報完成':'Completion just reported');
  }
  function roster(shown){
    const {rows,selected}=read(),container=$('register'),before=new Map([...container.querySelectorAll('.row')].map(e=>[e.dataset.ref,e]));
    const focus=document.activeElement?.dataset.ref,scroll=container.scrollTop,groups=new Map();
    const modeButton=$('kaiju-mode');modeButton.hidden=false;modeButton.textContent=mode==='files'?w().avatars:w().files;modeButton.setAttribute('aria-pressed',String(mode==='avatars'));
    modeButton.onclick=()=>{mode=mode==='files'?'avatars':'files';roster(shown);};
    $('roster-title').textContent=w().roster;container.className='register kaiju-register '+mode;
    for(const r of shown){const key=projectKey(r);if(!groups.has(key))groups.set(key,{label:projectName(r),rows:[]});groups.get(key).rows.push(r);}
    const sections=[];
    for(const [key,group] of [...groups].sort((a,b)=>read().projectOrder.indexOf(a[0])-read().projectOrder.indexOf(b[0]))){
      const section=el('section',undefined,'kaiju-roster-group'),heading=el('button',group.label,'kaiju-project-heading');
      const color=meta.city.slots[read().projectOrder.indexOf(key)%meta.city.slots.length].color;section.style.setProperty('--kaiju-slot-color',color);section.style.setProperty('--kaiju-portrait-bg',color+'40');
      heading.setAttribute('aria-pressed',String(read().selectedProject===key));heading.onclick=()=>projectSelect(key);section.append(heading);
      if(read().selectedProject===key){const controls=renderCast(key);if(controls)section.append(controls);}
      const grid=el('div',undefined,'kaiju-cards');
      for(const r of group.rows){
        const b=before.get(r.ref)||el('button');b.className='row kaiju-file'+(selected===r.ref?' selected':'')+(state(r)==='working'?' working':'');b.dataset.ref=r.ref;
        b.setAttribute('aria-pressed',String(selected===r.ref));b.setAttribute('aria-label',r.title+' · '+status(r)+' · '+freshness(r));b.title=r.title+' · '+status(r)+' · '+freshness(r);
        b.style.setProperty('--kaiju-tone',callback(r)?'#83d6d8':state(r)==='working'?'#e07a3c':state(r)==='waiting_you'?'#c99a1e':['milestone_complete','work_complete'].includes(state(r))?'#d9573f':'#8a8f99');
        b.dataset.fresh=String(fresh(r));b.replaceChildren();
        const number=el('span','No.'+String(rows.findIndex(row=>row.ref===r.ref)+1).padStart(3,'0'),'kaiju-folder-tab');
        const paper=el('span',undefined,'kaiju-paper'),top=el('span',undefined,'kaiju-file-top');top.append(el('span',w().file),el('span',status(r),'kaiju-stamp'));
        const identity=el('span',undefined,'kaiju-identity'),name=el('span',undefined,'kaiju-identity-name');name.append(el('b',w().monster[assignment(r.ref)]),el('span',projectName(r)));identity.append(portrait(r),name);
        const field=(label,value,cls='')=>{const row=el('span',undefined,'kaiju-field '+cls);row.append(el('span',label,'kaiju-field-label'),typeof value==='string'?el('span',value):value);return row;};
        const damage=wear.get(key)??0,bar=el('span',undefined,'kaiju-damage');bar.setAttribute('aria-label',w().damageNames[damage]);
        for(let i=0;i<4;i++){const block=el('i');block.dataset.filled=String(i<=damage);block.style.setProperty('--damage-color',['#5fae6e','#d8a548','#e07a3c','#d9573f'][damage]);bar.append(block);}bar.append(el('span',w().damageNames[damage]));
        paper.append(top,identity,field(w().task,r.title,'kaiju-task name'),field(w().condition,status(r)+' · '+freshness(r)),field(w().damage,bar));
        const elapsed=time(r);if(elapsed){const value=el('span',elapsed,'kaiju-elapsed');value.dataset.ref=r.ref;paper.append(field(w().time,value));}
        b.append(number,paper);b.onclick=()=>{choose(r.ref);if(matchMedia('(max-width:700px)').matches)$('scene').parentElement.scrollIntoView({block:'nearest'});};
        grid.append(b);
      }
      section.append(grid);sections.push(section);
    }
    container.replaceChildren(...sections);container.scrollTop=scroll;
    if(focus)container.querySelectorAll('.row').forEach(b=>{if(b.dataset.ref===focus)b.focus({preventScroll:true});});
  }
  function news(){
    const {rows,selected}=read();items=items.filter(ref=>ref!==selected&&rows.some(r=>r.ref===ref&&fresh(r)&&hot(r)&&!r.focused));
    const box=$('kaiju-news');box.setAttribute('aria-label',w().news);box.hidden=!items.length;
    const signature=JSON.stringify([read().context.language,read().night,items.map(ref=>{const r=rows.find(row=>row.ref===ref);return[ref,r.title,projectName(r),state(r)];})]);
    if(newsSignature===signature)return;newsSignature=signature;box.replaceChildren();if(!items.length)return;
    const heading=el('div',undefined,'kaiju-news-heading'),clear=el('button','×');clear.setAttribute('aria-label',w().clear);clear.onclick=()=>{items=[];news();};heading.append(el('b',w().news),clear);box.append(heading);
    for(const ref of items.slice(0,meta.newsRows)){
      const r=rows.find(r=>r.ref===ref),button=el('button',undefined,'kaiju-news-row'+(newsPainted.has(ref)?'':' is-new')),img=el('img');img.src=asset(monster(r).face);img.alt='';img.width=32;img.height=32;
      const caption=el('span');caption.append(el('b',state(r)==='waiting_you'?w().waiting:w().completed),el('span',projectName(r)+' · '+r.title));button.append(img,caption);button.onclick=()=>{items=items.filter(item=>item!==ref);choose(ref);};box.append(button);newsPainted.add(ref);
    }
    if(items.length>meta.newsRows)box.append(el('p',w().more.replace('{count}',String(items.length-meta.newsRows))));
  }
  function setWear(key,level){
    if(wear.get(key)===level)return;wear.set(key,level);
    for(const b of $('register').querySelectorAll('.kaiju-file')){
      const r=read().rows.find(row=>row.ref===b.dataset.ref);if(!r||projectKey(r)!==key)continue;
      const bar=b.querySelector('.kaiju-damage');if(!bar)continue;
      bar.setAttribute('aria-label',w().damageNames[level]);bar.querySelector('span').textContent=w().damageNames[level];bar.querySelectorAll('i').forEach((block,i)=>{block.dataset.filled=String(i<=level);block.style.setProperty('--damage-color',['#5fae6e','#d8a548','#e07a3c','#d9573f'][level]);});
    }
  }
  function tick(){
    const now=Math.floor(Date.now()/60000);if(now===minute)return;minute=now;
    for(const node of $('register').querySelectorAll('.kaiju-elapsed')){const r=read().rows.find(row=>row.ref===node.dataset.ref);if(r)node.textContent=time(r)??w().unknownTime;}
  }
  return {assignment,reset,sync,roster,news,setWear,tick,status,selected(ref){items=items.filter(item=>item!==ref);news();}};
}
