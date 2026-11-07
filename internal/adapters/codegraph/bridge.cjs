'use strict';
// The parent passes only a reviewed bundle, one canonical root and registered
// limits. Project modules, hooks and provider credentials are never loaded.
const fs = require('fs');
const path = require('path');
const crypto = require('crypto');
const os = require('os');
const {spawnSync} = require('child_process');
const readline = require('readline');
const [bundle, mode, rootArg, limitsArg] = process.argv.slice(2);
const L = JSON.parse(limitsArg);
const root = fs.realpathSync(rootArg);
const lib = path.join(bundle, 'lib', 'dist');
console.log = (...args) => console.error(...args);
const {CodeGraph, FileWatcher, setLogger, silentLogger} = require(path.join(lib, 'index.js'));
const {scanDirectory, hashContent} = require(path.join(lib, 'extraction', 'index.js'));
const {readBoundedSourceSync, oversizeStamp} = require(path.join(lib, 'file-limits.js'));
const {hasMpegTsExtension,isMpegTransportStream,MPEG_TS_SNIFF_BYTES} = require(path.join(lib,'extraction','grammars.js'));
const {clearProjectConfigCache} = require(path.join(lib,'project-config.js'));
const {ToolHandler} = require(path.join(lib, 'mcp', 'tools.js'));
setLogger(silentLogger);
const excluded = new Set(['.git', '.codegraph', 'graphify-out', 'node_modules', 'venv', '.venv', 'dist', 'build', 'target', '__pycache__']);
const extensions = new Set(L.extensions.trim().split(/\s+/));
let cg, watcher, handler, timer;
let busy = false;
let closed = false;
let ready = false;
let fault = '';
let configuration = '';
let lastIndexed = 0;
const ownerPath = path.join(root, '.codegraph', 'clawdline.json');
const send = value => process.stdout.write(JSON.stringify(value) + '\n');
const fail = code => {const error = new Error(code); error.code = code; return error;};
const hash = value => crypto.createHash('sha256').update(value).digest('hex');
function sourceName(name, activeExtensions) {
 const base = path.basename(name);
 return ['.gitignore', '.codegraphignore', 'Gemfile', 'Cargo.lock'].includes(base) || name==='conf/routes' || name.endsWith('/conf/routes') || name.endsWith('.routes') || /\.app(?:\.src)?$/i.test(name) || activeExtensions.has(path.extname(name).toLowerCase());
}
function safeFile(relative) {
 if (path.isAbsolute(relative) || relative.split(/[\\/]/).includes('..')) throw fail('unsafe_source');
 let component = root;
 for(const part of relative.split(/[\\/]/)){
  component=path.join(component,part);
  if(fs.lstatSync(component).isSymbolicLink())throw fail('unsafe_source');
 }
 const absolute = path.join(root, relative);
 const actual = fs.realpathSync(absolute);
 if (actual !== root && !actual.startsWith(root + path.sep)) throw fail('unsafe_source');
 const st = fs.lstatSync(absolute);
 if (!st.isFile() || st.isSymbolicLink()) throw fail('unsafe_source');
 if (st.size > L.fileBytes) throw fail('source_limit');
 return {absolute, st};
}
function boundedContent(absolute) {
 const fd=fs.openSync(absolute,fs.constants.O_RDONLY | (fs.constants.O_NOFOLLOW || 0));
 try{
  const st=fs.fstatSync(fd);
  if(!st.isFile())throw fail('unsafe_source');
  if(st.size>L.fileBytes)throw fail('source_limit');
  const chunks=[];let size=0;
  while(size<=L.fileBytes){
   const chunk=Buffer.allocUnsafe(Math.min(L.chunkBytes,L.fileBytes+1-size));
   const count=fs.readSync(fd,chunk,0,chunk.length,null);
   if(!count)break;
   size+=count;chunks.push(chunk.subarray(0,count));
  }
  if(size>L.fileBytes)throw fail('source_limit');
  return Buffer.concat(chunks,size);
 }finally{fs.closeSync(fd)}
}
// Same arguments and environment as the Go check (ignored.go), so both
// compute one inventory: only the repository's own ignore files decide, and
// settings that make Git run a program are emptied on the command line.
function ignoredPaths() {
 const out = {files:new Set(), dirs:new Set()};
 // A build parses the private snapshot the Go scan already filtered; Git there
 // could only answer for whatever repository happens to enclose the temp directory.
 if (mode === 'build') return out;
 const env = {GIT_CONFIG_NOSYSTEM:'1', GIT_CONFIG_GLOBAL:os.devNull, GIT_TERMINAL_PROMPT:'0', LC_ALL:'C'};
 for (const name of ['PATH','HOME','USERPROFILE','SYSTEMROOT']) if (process.env[name] !== undefined) env[name] = process.env[name];
 const result = spawnSync('git', ['-c','core.fsmonitor=','-c','core.excludesFile=','-c','core.hooksPath='+os.devNull,'ls-files','-z','--others','--ignored','--exclude-standard','--directory'],
  {cwd:root, env, maxBuffer:L.ignoredBytes, encoding:'buffer', windowsHide:true});
 if (result.error?.code === 'ENOENT') return out;
 if (result.error?.code === 'ENOBUFS') throw fail('source_limit');
 if (result.error) throw fail('ignore_unavailable');
 if (result.status !== 0) {
  if (String(result.stderr).includes('not a git repository')) return out;
  throw fail('ignore_unavailable');
 }
 for (const name of result.stdout.toString('utf8').split('\0')) {
  if (!name) continue;
  if (name.endsWith('/')) out.dirs.add(name.slice(0,-1)); else out.files.add(name);
 }
 return out;
}
function inventory() {
 // Re-read custom mappings even for same-mtime configuration changes.
 clearProjectConfigCache(root);
 const activeExtensions=new Set(extensions);
 if(fs.existsSync(path.join(root,'codegraph.json'))){
  const {absolute}=safeFile('codegraph.json');
  const content=boundedContent(absolute).toString('utf8');
  let config;try{config=JSON.parse(content);}catch{}
  if(config?.extensions && typeof config.extensions==='object' && !Array.isArray(config.extensions)){
   for(const [key,language] of Object.entries(config.extensions)){
    const body=key.trim().toLowerCase().replace(/^\./,'');
    if(body && !/[.\/\\]/.test(body) && (L.languages || []).includes(language))activeExtensions.add('.'+body);
   }
  }
 }
 const ignored = ignoredPaths();
 const sources = {};
 let entries = 0, bytes = 0;
 function walk(dir) {
  for (const entry of fs.readdirSync(path.join(root, dir), {withFileTypes: true})) {
   if (++entries > L.entries) throw fail('entry_limit');
   const name = dir ? dir + '/' + entry.name : entry.name;
   if (entry.isDirectory()) {
    if (!excluded.has(entry.name) && !entry.name.startsWith('.clawdline-codegraph-') && !ignored.dirs.has(name)) walk(name);
   } else if (!ignored.files.has(name) && sourceName(name,activeExtensions)) {
    const {absolute,st} = safeFile(name);
    const data=boundedContent(absolute);
    bytes += data.length;
    if (bytes > L.totalBytes) throw fail('source_limit');
    sources[name] = hash(data);
   }
  }
 }
 walk('');
 const sorted = Object.keys(sources).sort().map(name => [name, sources[name]]);
 const configs = sorted.filter(([name]) => !['.go','.ts','.tsx','.mts','.cts','.js','.jsx','.mjs','.cjs','.py','.rs','.java','.c','.h','.cpp','.cs','.rb','.php','.swift','.kt'].includes(path.extname(name).toLowerCase()));
 return {sources, generation:hash(JSON.stringify(sorted)), configuration:hash(JSON.stringify(configs)), entries, bytes};
}
function nativeChanges() {
 // Full content checks avoid relying on Git's stat cache or an mtime equality.
 const files = scanDirectory(root);
 if (files.length > L.entries) throw fail('entry_limit');
 const tracked = new Map(cg.getFiles().map(file => [file.path, file]));
 const current = new Set();
 const changed = [];
 let bytes = 0;
 for (const name of files) {
  const {absolute,st} = safeFile(name);
  bytes += st.size;
  if (bytes > L.totalBytes) throw fail('source_limit');
  const {stats,bytes:sourceBytes} = readBoundedSourceSync(absolute);
  const content = sourceBytes === null ? oversizeStamp(stats.size) : hasMpegTsExtension(absolute) && isMpegTransportStream(sourceBytes.subarray(0,MPEG_TS_SNIFF_BYTES)) ? null : sourceBytes.toString('utf8');
  if (content === null) continue;
  current.add(name);
  if (!tracked.has(name) || tracked.get(name).contentHash !== hashContent(content)) changed.push(name);
 }
 for (const name of tracked.keys()) if (!current.has(name)) changed.push(name);
 return [...new Set(changed)];
}
function diskGuard() {
 let bytes = 0, entries = 0;
 function walk(dir) {
  for (const entry of fs.readdirSync(dir,{withFileTypes:true})) {
   if (++entries > L.entries) throw fail('runtime_limit');
   const p = path.join(dir,entry.name);
   if (entry.isSymbolicLink()) throw fail('unsafe_source');
   if (entry.isDirectory()) walk(p);
   else {
    try { bytes += fs.statSync(p).size; } catch(e) { if(e.code==='ENOENT')continue; throw e; }
    if (bytes > L.storeBytes) throw fail('runtime_limit');
   }
  }
 }
 walk(path.join(root,'.codegraph'));
}
function save(current) {
 diskGuard();
 const document = {schema:1,root,version:L.version,generation:current.generation,configuration:current.configuration,sources:current.sources};
 const text = JSON.stringify(document);
 if (Buffer.byteLength(text) > L.metadataBytes) throw fail('source_limit');
 const candidate = ownerPath + '.candidate';
 fs.writeFileSync(candidate,text,{mode:0o600});
 fs.renameSync(candidate,ownerPath);
 lastIndexed = Date.now();
}
async function reconcile() {
 if (closed) throw fail('stopped');
 if (busy || cg?.isIndexing()) throw fail('busy');
 busy = true;
 try {
  const before = inventory();
  if(before.configuration !== configuration)clearProjectConfigCache();
  const changed = nativeChanges();
  if (before.configuration !== configuration || cg.isIndexStale() || cg.getIndexState() !== 'complete') {
   // A full sync prunes rows leaving the configured scope; indexAll alone
   // may retain previously indexed files that are now excluded.
   await cg.sync();
   await cg.indexAll();
  } else if (changed.length) {
   await cg.sync({paths:changed});
   // Upstream sync skips an equal-size/equal-mtime file before hashing it.
   // Our independent content comparison detects that blind spot; indexFiles
   // bypasses the stat fast path without changing the person’s source metadata.
   const residual = nativeChanges();
   if(residual.length) await cg.indexFiles(residual.filter(name=>fs.existsSync(path.join(root,name))));
  }
  const after = inventory();
  if (before.generation !== after.generation || nativeChanges().length) throw fail('source_changed');
  configuration = after.configuration;
  save(after);
  fault = '';
  return {filesChanged:changed.length,durationMs:0};
 } catch(e) { fault=e.code || 'parser_failed'; throw e; }
 finally { busy=false; }
}
function status() {
 diskGuard();
 const current = inventory();
 const dirty = busy || cg.isIndexing() || cg.getIndexState()!=='complete' || nativeChanges().length > 0 || (fault && fault!=='watch_unavailable') || current.configuration !== configuration;
 return {state:dirty?'stale':'source_matched',reason:dirty?'codegraph_source_changed':fault?'codegraph_'+fault:'codegraph_sources_match',generation:current.generation,watching:!!watcher?.isActive(),last_synced_at:Math.floor(lastIndexed/1000),files:Object.keys(current.sources).length,bytes:current.bytes};
}
async function stop() {
 if(closed)return;
 closed=true;
 clearInterval(timer);
 watcher?.stop();
 // The owner closes stdin to revoke the process. Never orphan native workers.
 try { cg?.close(); } finally { process.exit(0); }
}
process.stdin.on('end',stop);
process.on('SIGTERM',stop);
process.on('SIGINT',stop);

async function main() {
 inventory();
 if (mode==='build') {
  if (fs.existsSync(path.join(root,'.codegraph'))) throw fail('index_exists');
  cg=await CodeGraph.init(root,{index:false});
  await cg.indexAll({onProgress:p=>send({event:'progress',phase:'parsing',files:p.filesProcessed??p.current??0})});
  if(cg.getIndexState()!=='complete')throw fail('parser_failed');
  diskGuard();
  fs.writeFileSync(path.join(root,'.codegraph','clawdline-build.json'),JSON.stringify({configuration:inventory().configuration}));
  cg.close();process.exit(0);
 }
 const stored=JSON.parse(fs.readFileSync(ownerPath,'utf8'));
 if(stored.root!==root || stored.version!==L.version)throw fail('index_unverified');
 cg=await CodeGraph.open(root,{sync:false});
 configuration=stored.configuration || '';
 await reconcile();
 handler=new ToolHandler(cg);
 watcher=new FileWatcher(root,async()=>reconcile(),{onDegraded:()=>{fault='watch_unavailable';},onSyncError:()=>{fault='parser_failed';}});
 watcher.start();
 await watcher.waitUntilReady();
 timer=setInterval(()=>{if(!busy)reconcile().catch(()=>{});},L.reconcileSeconds*1000);
 ready=true;
 send({event:'ready'});
}
 // Start the sole reader before main's first await, including build mode.
 const lines=readline.createInterface({input:process.stdin});
 lines.on('close',stop);
 lines.on('line',async line=>{
  let id;
  try {
   if(Buffer.byteLength(line)>L.replyBytes)throw fail('source_limit');
   const request=JSON.parse(line);id=request.id;
   if(!ready)throw fail('busy');
   if(request.method==='status'){send({id,result:status()});return;}
   if(request.method==='sync'){await reconcile();send({id,result:status()});return;}
   if(request.method!=='query' || typeof request.query!=='string' || Buffer.byteLength(request.query)>L.queryBytes)throw fail('bad_request');
   if(busy)throw fail('busy');
   const before=status();if(before.state!=='source_matched'){console.error('query refused before',JSON.stringify(before));throw fail('source_changed');}
   busy=true;
   let answer;
   try{answer=await handler.execute('codegraph_explore',{query:request.query});}finally{busy=false;}
   const after=status();
   if(after.state!=='source_matched' || after.generation!==before.generation){console.error('query refused after',JSON.stringify({before,after}));throw fail('source_changed');}
   if(answer.isError)throw fail('query_failed');
   const text=answer.content.filter(x=>x.type==='text').map(x=>x.text).join('\n');
   if(Buffer.byteLength(text)>L.replyBytes/2)throw fail('reply_limit');
   send({id,result:{text,generation:after.generation}});
  }catch(e){send({id,error:e.code || 'parser_failed'});}
 });
main().catch(e=>{console.error(e.stack || String(e));send({event:'failed',error:e.code || 'parser_failed'});process.exit(1);});
