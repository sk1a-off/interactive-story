// DOM contract tests, not a substitute for a live userscript-manager test.
import {readFileSync} from 'node:fs';
import {createRequire} from 'node:module';
import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createHash,webcrypto} from 'node:crypto';
const require=createRequire(new URL('../frontend/package.json',import.meta.url));
const {JSDOM,VirtualConsole}=require('jsdom');
const source=readFileSync(new URL('../backend/cmd/chatgptbridge/web/worker.user.js',import.meta.url),'utf8');
const pause=ms=>new Promise(resolve=>setTimeout(resolve,ms));
async function until(check){for(let i=0;i<500;i++){if(check())return;await pause(2);}throw Error('test timed out');}
function fixture(options={}){
 const dom=new JSDOM(`<main><header>Временный чат</header><button aria-label="Выйти из режима временного чата"></button><button aria-label="Без персонализации" aria-haspopup="menu"></button><div role="textbox" contenteditable="true"></div><button aria-label="Отправить"></button><button aria-label="Выбрать модель ChatGPT" aria-expanded="false">Medium</button><section id="transcript"></section></main>`,{url:'https://chatgpt.com/?temporary-chat=true#story-bridge-worker',runScripts:'outside-only',virtualConsole:new VirtualConsole()});
 const w=dom.window;let clock=0;const requests=[];let clicks=0;let personalizationClicks=0;
 w.Date.now=()=>clock;w.setTimeout=(fn,ms)=>setTimeout(()=>{clock+=ms;fn();},0);
 w.Element.prototype.getClientRects=()=>[{}];
 w.PointerEvent=w.MouseEvent;
 Object.defineProperty(w.HTMLElement.prototype,'innerText',{get(){return this.textContent;},set(value){this.textContent=value;}});
 const j={textOnly:!!options.textOnly,reasoningEffort:options.reasoningEffort||'none',recoverSafeFailure:!!options.recoverSafeFailure,id:'job-id',worker:'test-worker',lease:'lease-secret',prompt:options.truncatedMessage?'long synthetic prompt '.repeat(650).trim():'synthetic prompt'};
 if(options.readable){
  j.messageFormat='readable-json-v3';j.messageMarker='Story request job-id';
  j.messageContent='SYSTEM INSTRUCTIONS\nUse `goal` and **bold**.\n\nUSER INPUT\n{"action":"Осмотреть дверь", "text":"Первая строка\\nВторая строка"}';
  if(options.truncatedMessage)j.messageContent+='\n'+'Длинный текст '.repeat(1500);
  j.messageContent=j.messageContent.trim();
  j.prompt=j.messageMarker+'\nExecute the block.\n```text\n'+j.messageContent+'\n```';
 }
 if(options.attachment){
  const content='SYSTEM INSTRUCTIONS\nВерни JSON.\nUSER INPUT\nПроверить дверь.';
  j.messageFormat='attachment-txt-v1';j.messageMarker='Story request job-id';j.prompt='Story request job-id. Execute attached story-job-id.txt.';
  j.attachment={name:'story-job-id.txt',content,bytes:Buffer.byteLength(content),sha256:createHash('sha256').update(content).digest('hex')};
  if(options.badFileHash)j.attachment.sha256='0'.repeat(64);
  w.TextEncoder=TextEncoder;w.File=File;Object.defineProperty(w.crypto,'subtle',{value:webcrypto.subtle});
  w.DataTransfer=class{constructor(){this.files=[];this.items={add:file=>this.files.push(file)}}};
  if(!options.missingFileInput){
   const input=w.document.createElement('input');input.type='file';input.multiple=true;input.accept='.txt';
   let files=[];Object.defineProperty(input,'files',{get:()=>files,set:value=>{files=value}});
   input.onchange=()=>{
    const card=w.document.createElement('div');card.id='upload-card';card.textContent=files[0].name;
    const remove=w.document.createElement('button');remove.setAttribute('aria-label','Remove file');card.append(remove);w.document.querySelector('main').append(card);
    if(options.uploadPending)card.setAttribute('aria-busy','true');
    if(options.uploadError){const alert=w.document.createElement('div');alert.setAttribute('role','alert');alert.textContent='Upload failed';card.append(alert);}
   };
   w.document.querySelector('main').append(input);
  }
 }
 let expansionClicks=0;
 const box=w.document.querySelector('[contenteditable]');
 w.document.querySelector('button[aria-haspopup="menu"]').onclick=()=>{personalizationClicks++;};
 const picker=w.document.querySelector('button[aria-label="Выбрать модель ChatGPT"]');
 function updatePicker(text){
  if(!options.hiddenPlaceholder){picker.textContent=text;return;}
  // Sanitized structure observed in the live ChatGPT DOM on 2026-11-10.
  const hidden=w.document.createElement('span');hidden.setAttribute('aria-hidden','true');hidden.style.opacity='0';hidden.textContent='Уровень усилий рассуждения';
  const hiddenOld=w.document.createElement('span');hiddenOld.setAttribute('aria-hidden','true');hiddenOld.textContent='Instant';
  const visible=w.document.createElement('span');visible.setAttribute('aria-hidden','false');visible.textContent=text;
  picker.replaceChildren(hidden,hiddenOld,visible);
 }
 let openingAttempts=0;
 let selectedModel=options.modelAlreadySelected?'GPT-6':'GPT-5.6 Sol';
 let effort=options.effortInitially??(options.instantInitially?0:1);
 const modeLabel=()=>['Instant','Medium','High'][effort];
 function renderSimple(menu){
  menu.replaceChildren();
  const toggle=w.document.createElement('div');toggle.dataset.modelPickerViewToggle='true';toggle.textContent=modeLabel();menu.append(toggle);
  toggle.onclick=()=>{
   menu.replaceChildren();
   if(options.modelUnavailable)return;
   const model=w.document.createElement('div');model.setAttribute('role','menuitemradio');model.setAttribute('aria-checked',String(selectedModel==='GPT-6'));model.textContent='GPT-6';menu.append(model);
   model.onclick=()=>{if(!options.modelSelectionIgnored)selectedModel='GPT-6';renderSimple(menu);};
  };
  const power=w.document.createElement('div');power.dataset.reasoningSlider='true';
  const track=w.document.createElement('div');track.dataset.modelPickerPowerSlider='';
  const slider=w.document.createElement('span');slider.setAttribute('role','slider');slider.setAttribute('aria-hidden','true');slider.setAttribute('aria-valuemin','0');slider.setAttribute('aria-valuemax',options.unknownSlider?'4':'2');slider.setAttribute('aria-valuenow',String(effort));track.append(slider);
  const status=w.document.createElement('span');status.setAttribute('role','status');status.textContent=effort===0?'Instant, 1 из 3.':'Medium, 2 из 3.';
  power.onkeydown=e=>{if(!options.ignoreReasoning&&['ArrowLeft','ArrowRight'].includes(e.key)){effort=Math.max(0,Math.min(2,effort+(e.key==='ArrowLeft'?-1:1)));slider.setAttribute('aria-valuenow',String(effort));status.textContent=modeLabel();updatePicker(modeLabel());toggle.textContent=modeLabel();}};
  menu.append(power,track);if(!options.noAnnouncement)menu.append(status);
 }
 const togglePicker=()=>{
  const existing=w.document.querySelector('[role="menu"]');
  if(existing){if(!options.triggerClickCannotClose){existing.remove();picker.setAttribute('aria-expanded','false');}return;}
  picker.setAttribute('aria-expanded','true');
  const menu=w.document.createElement('div');menu.setAttribute('role','menu');renderSimple(menu);w.document.body.append(menu);
 };
 if(options.triggerPointerOnly){picker.onclick=()=>{};picker.addEventListener('pointerdown',togglePicker);}else{picker.onclick=togglePicker;}
 picker.addEventListener('keydown',e=>{if(e.key==='ArrowDown'&&!w.document.querySelector('[role="menu"]')&&++openingAttempts>(options.ignoredOpens||0))togglePicker();});
 const closeMenu=()=>{w.document.querySelector('[role="menu"]')?.remove();picker.setAttribute('aria-expanded','false');if(options.closedWrongMode)updatePicker('Medium');};
 w.document.addEventListener('keydown',e=>{if(e.key==='Escape'&&!options.ignoreEscape)closeMenu();});
 w.document.addEventListener('pointerdown',e=>{if(e.target===box&&!options.ignoreOutside)closeMenu();});
 updatePicker(modeLabel());
 if(options.menuInitiallyOpen)picker.click();
 function unexpectedFile(){const card=w.document.createElement('div');card.dataset.testid='file-upload-thumbnail';const remove=w.document.createElement('button');remove.setAttribute('aria-label','Remove file');card.append(remove);w.document.querySelector('main').append(card);}
 w.document.execCommand=(_cmd,_ui,text)=>{box.textContent=text;if(options.autoAttachment)unexpectedFile();if(options.asyncAutoAttachment)setTimeout(unexpectedFile,0);return true;};
 w.GM_getValue=()=> 'a'.repeat(48);w.GM_setValue=()=>{};w.GM_registerMenuCommand=()=>{};
 w.GM_xmlhttpRequest=r=>{
  const path=new URL(r.url).pathname,body=JSON.parse(r.data);requests.push({path,body});
  if(path==='/send'&&options.lostAuthorization){queueMicrotask(()=>r.ontimeout());return;}
  if(path==='/send'&&options.fileDuringAuthorization)unexpectedFile();
  if(path==='/send'&&options.removeFileDuringAuthorization)w.document.querySelector('#upload-card')?.remove();
  if(path==='/send'&&options.stopDuringAuthorization)w.document.querySelector('#story-chatgpt-bridge button').click();
  let value={};if(path==='/claim')value=j;
  if(path==='/attachment')value={verified:!options.uploadAckRejected};
  if(path==='/fail')value={state:options.failedState||'uncertain'};
  if(path==='/result')value={state:options.rejectedResult?'failed':'complete'};
  queueMicrotask(()=>r.onload({status:200,responseText:JSON.stringify(value)}));
 };
 w.document.querySelector('button[aria-label="Отправить"]').onclick=()=>{
  clicks++;
  const user=w.document.createElement('div');const heading=w.document.createElement('h4');heading.dataset.conversationRole='user';
  const bubble=w.document.createElement('div');bubble.dataset.userMessageBubble='true';const content=w.document.createElement('div');content.dataset.searchResultTarget='';content.textContent=options.wrongUser?'unrelated prompt':options.truncatedMessage?j.prompt.slice(0,10240):j.prompt;bubble.append(content);if(options.expandableMessage||options.truncatedMessage){const more=w.document.createElement('button');more.textContent='Показать ещё';more.onclick=()=>{expansionClicks++;if(options.truncatedMessage&&!options.expansionBlocked){content.textContent=options.splitExpanded?j.prompt.slice(0,10240):j.prompt;if(options.splitExpanded){const rest=w.document.createElement('div');rest.dataset.searchResultTarget='';rest.textContent=j.prompt.slice(10240);bubble.insertBefore(rest,more);}}more.textContent='Показать меньше';};bubble.append(more);}user.append(heading,bubble);
  if(options.attachment){const card=w.document.querySelector('#upload-card');if(card&&!options.sentFileMissing)user.append(card);else card?.remove();}

  const assistant=w.document.createElement('div');const h=w.document.createElement('h4');h.dataset.conversationRole='assistant';
  const answer=w.document.createElement('div');answer.dataset.markdownTextStyle='assistant-message';answer.textContent=options.emptyAnswer?'':'{"probe_id":"job-id","ok":true}';assistant.append(h,answer);if(options.externalAnswer)assistant.after(answer);
  const copy=w.document.createElement('button');copy.setAttribute('aria-label','Копировать');
  w.document.querySelector('#transcript').append(user,assistant,copy);
  if(options.externalAnswer)w.document.querySelector('#transcript').append(answer);
  if(options.streamingEmpty){const stop=w.document.createElement('button');stop.setAttribute('aria-label','Stop generating');assistant.append(stop);}
  if(options.userCopyOnly){user.append(copy);}
  if(options.readable&&options.fragmentedInline){
   content.replaceChildren();
   const pieces=j.prompt.split('`');
   pieces.forEach((piece,i)=>{if(i%2){const code=w.document.createElement('code');code.textContent=piece;content.append(code);}else content.append(w.document.createTextNode(piece));});
   if(options.changedRestoredText)content.append(w.document.createTextNode(' changed'));
  }
  if(options.readable&&!options.rawEnvelope&&!options.fragmentedInline){
   content.textContent=(options.wrongMarker?'Story request other-job':j.messageMarker)+'\nExecute the block.\n';
   const code=w.document.createElement('code');content.append(code);
   code.textContent=options.wrongUser?'Осмотреть другую дверь':options.truncatedMessage?j.messageContent.slice(0,10200):j.messageContent;
   const more=bubble.querySelector('button');
   if(more)more.onclick=()=>{expansionClicks++;if(!options.expansionBlocked){code.textContent=j.messageContent;more.textContent='Свернуть';}};
  }

 };
 if(!options.unarmed)w.sessionStorage.setItem('story-chatgpt-probe-armed','yes');
 if(options.previousSafeFailures)w.sessionStorage.setItem('story-chatgpt-probe-safeFailures',String(options.previousSafeFailures));
 if(options.pending)w.sessionStorage.setItem('story-chatgpt-probe-pending',JSON.stringify({worker:j.worker,id:j.id,lease:j.lease}));
 if(options.personalized)w.document.querySelector('button[aria-haspopup="menu"]').setAttribute('aria-label','Персонализированный');
 if(options.noPersonalizationControl)w.document.querySelector('button[aria-haspopup="menu"]').remove();
 if(options.notTemporary){w.document.querySelector('header').textContent='Regular chat';w.document.querySelector('button').remove();}
 if(options.withoutMarker)w.history.replaceState(null,'','/?temporary-chat=true');
 if(options.registeredWorker)w.sessionStorage.setItem('story-chatgpt-probe-uuid','a624163e-3125-4bd7-bd3d-29aab5da74da');
 w.eval(source);
 return {w,requests,get clicks(){return clicks;},get expansionClicks(){return expansionClicks;},get personalizationClicks(){return personalizationClicks;},close:()=>dom.window.close()};
}
test('one send and completed reply correlated to current lease',async()=>{
 const f=fixture({personalized:true});try{await until(()=>f.requests.some(r=>r.path==='/result')).catch(e=>{throw Error(e.message+' '+f.w.document.querySelector('#story-chatgpt-bridge').textContent+' '+JSON.stringify(f.requests));});assert.equal(f.clicks,1);assert.deepEqual(f.requests.filter(r=>r.path!=='/heartbeat').map(r=>r.path),['/claim','/send','/result']);const body=f.requests.at(-1).body;assert.equal(body.id,'job-id');assert.equal(body.lease,'lease-secret');}finally{f.close();}
});
for(const options of [{lostAuthorization:true},{stopDuringAuthorization:true},{wrongUser:true},{rejectedResult:true},{pending:true},{notTemporary:true},{modelUnavailable:true},{modelSelectionIgnored:true},{hiddenPlaceholder:true,closedWrongMode:true},{triggerClickCannotClose:true,ignoreEscape:true,ignoreOutside:true}])test('safe stop '+JSON.stringify(options),async()=>{
 const f=fixture(options);try{await until(()=>f.w.sessionStorage.getItem('story-chatgpt-probe-armed')===null);assert.ok(f.clicks<=((options.wrongUser||options.rejectedResult)?1:0));if(!options.rejectedResult)assert.ok(!f.requests.some(r=>r.path==='/result'));assert.ok(f.requests.filter(r=>r.path==='/send').length<=1);if(options.pending||options.notTemporary)assert.ok(!f.requests.some(r=>r.path==='/claim'));}finally{f.close();}
});

// Regression: a newly opened marked tab is not armed. Clicking Start at the
// exact fresh-chat URL must run without depending on a navigation event.
test('Start on the already open fresh-chat URL starts the worker immediately',async()=>{
 const f=fixture({unarmed:true,personalized:true});
 try{
  const start=f.w.document.querySelector('#story-chatgpt-bridge button');
  assert.equal(start.textContent,'Запустить');assert.equal(f.requests.length,0);
  start.click();
  assert.equal(start.textContent,'Остановить');
  assert.equal(f.w.sessionStorage.getItem('story-chatgpt-probe-armed'),'yes');
  await until(()=>f.requests.some(r=>r.path==='/result'));
  assert.equal(f.clicks,1);
 }finally{f.close();}
});
test('Start also works when only the worker fragment was removed',async()=>{
 const f=fixture({unarmed:true});
 try{
  f.w.history.replaceState(null,'','/?temporary-chat=true');
  f.w.document.querySelector('#story-chatgpt-bridge button').click();
  await until(()=>f.requests.some(r=>r.path==='/result'));
  assert.equal(f.clicks,1);
 }finally{f.close();}
});

for(const options of [{personalized:true},{noPersonalizationControl:true}])test('personalization never blocks or changes the chat '+JSON.stringify(options),async()=>{
 const f=fixture({...options,unarmed:true});
 try{
  f.w.document.querySelector('#story-chatgpt-bridge button').click();
  await until(()=>f.requests.some(r=>r.path==='/result'));
  assert.equal(f.clicks,1);assert.equal(f.personalizationClicks,0);
  if(options.personalized)assert.ok(f.w.document.querySelector('button[aria-label="Персонализированный"]'));
 }finally{f.close();}
});

for(const options of [{noAnnouncement:true},{noAnnouncement:true,instantInitially:true,modelAlreadySelected:true,menuInitiallyOpen:true}])test('verify GPT-6 before the model list unmounts, then Instant without a live announcement '+JSON.stringify(options),async()=>{
 const f=fixture(options);
 try{
  await until(()=>f.requests.some(r=>r.path==='/result'));
  assert.equal(f.clicks,1);
  assert.equal(f.w.document.querySelector('[role="menu"]'),null);
  assert.equal(f.w.document.querySelector('button[aria-label="Выбрать модель ChatGPT"]').textContent,'Instant');
 }finally{f.close();}
});

for(const options of [{triggerClickCannotClose:true},{triggerClickCannotClose:true,ignoreEscape:true}])test('dismiss model menu without clicking its trigger '+JSON.stringify(options),async()=>{
 const f=fixture(options);
 try{
  await until(()=>f.requests.some(r=>r.path==='/result'));
  assert.equal(f.clicks,1);assert.equal(f.w.document.querySelector('[role="menu"]'),null);
 }finally{f.close();}
});

test('hidden animation placeholder does not reject the visible Instant control',async()=>{
 const f=fixture({hiddenPlaceholder:true});
 try{
  await until(()=>f.requests.some(r=>r.path==='/result'));
  assert.equal(f.clicks,1);
  const picker=f.w.document.querySelector('button[aria-label="Выбрать модель ChatGPT"]');
  assert.ok(picker.innerText.includes('Уровень усилий рассуждения'));
  assert.equal(picker.querySelector('[aria-hidden="false"]').textContent,'Instant');
 }finally{f.close();}
});
test('control diagnostics exclude the prompt and transport credentials',async()=>{
 const f=fixture({hiddenPlaceholder:true,closedWrongMode:true});
 try{
  await until(()=>f.w.sessionStorage.getItem('story-chatgpt-probe-armed')===null);
  [...f.w.document.querySelectorAll('#story-chatgpt-bridge button')].find(e=>e.textContent==='Диагностика').click();
  await pause(10);
  const text=f.w.document.querySelector('#story-chatgpt-bridge p').textContent;
  assert.ok(text.startsWith('Диагностика: '));
  const report=JSON.parse(text.slice('Диагностика: '.length));
  assert.equal(report.version,'0.4.1');assert.equal(report.lastModelFailure.stage,'закрытие меню модели');
  assert.equal(report.current.picker.text,'Medium');
  for(const secret of ['synthetic prompt','lease-secret','a'.repeat(48)])assert.ok(!text.includes(secret));
 }finally{f.close();}
});

test('Show more inside a user bubble is excluded from exact prompt correlation',async()=>{
 const f=fixture({expandableMessage:true});
 try{await until(()=>f.requests.some(r=>r.path==='/result'));assert.equal(f.clicks,1);assert.ok(f.w.document.querySelector('[data-user-message-bubble]').textContent.includes('Показать ещё'));}finally{f.close();}
});
test('expandable messages still reject a different user prompt',async()=>{
 const f=fixture({expandableMessage:true,wrongUser:true});
 try{
  await until(()=>f.w.sessionStorage.getItem('story-chatgpt-probe-armed')===null);
  assert.ok(!f.requests.some(r=>r.path==='/result'));
  [...f.w.document.querySelectorAll('#story-chatgpt-bridge button')].find(e=>e.textContent==='Диагностика').click();await pause(5);
  const text=f.w.document.querySelector('#story-chatgpt-bridge p').textContent;
  const report=JSON.parse(text.slice('Диагностика: '.length));assert.equal(report.lastFailure.userMessage.matches,false);assert.ok(!text.includes('unrelated prompt'));
 }finally{f.close();}
});

test('ordinary unmarked tabs have no worker panel or network activity',async()=>{
 const f=fixture({unarmed:true,withoutMarker:true});try{assert.equal(f.w.document.querySelector('#story-chatgpt-bridge'),null);assert.equal(f.requests.length,0);}finally{f.close();}
});
test('registered worker keeps its paused panel when ChatGPT removes the URL fragment',async()=>{
 const f=fixture({unarmed:true,withoutMarker:true,registeredWorker:true});
 try{assert.equal(f.requests.length,0);f.w.document.querySelector('#story-chatgpt-bridge button').click();await until(()=>f.requests.some(r=>r.path==='/result'));assert.equal(f.clicks,1);}finally{f.close();}
});

test('model trigger driven by pointerdown opens before selecting a model',async()=>{
 const f=fixture({triggerPointerOnly:true});try{await until(()=>f.requests.some(r=>r.path==='/result'));assert.equal(f.clicks,1);}finally{f.close();}
});

test('expand a 10240-character prefix and require the entire prompt before accepting a reply',async()=>{
 const f=fixture({truncatedMessage:true});try{await until(()=>f.requests.some(r=>r.path==='/result'));assert.equal(f.clicks,1);assert.equal(f.expansionClicks,1);}finally{f.close();}
});
test('an unexpandable matching prefix never authorizes a result',async()=>{
 const f=fixture({truncatedMessage:true,expansionBlocked:true});try{await until(()=>f.w.sessionStorage.getItem('story-chatgpt-probe-armed')===null);assert.ok(!f.requests.some(r=>r.path==='/result'));assert.equal(f.clicks,1);assert.equal(f.expansionClicks,1);}finally{f.close();}
});

test('expanded user-message segments are combined without accepting only the first chunk',async()=>{
 const f=fixture({truncatedMessage:true,splitExpanded:true});try{await until(()=>f.requests.some(r=>r.path==='/result'));assert.equal(f.clicks,1);assert.equal(f.expansionClicks,1);}finally{f.close();}
});

test('delayed menu handlers are retried before sending, with one model request',async()=>{
 const f=fixture({ignoredOpens:2});try{await until(()=>f.requests.some(r=>r.path==='/result'));assert.equal(f.clicks,1);}finally{f.close();}
});
test('benchmark safe pre-send failure moves to a fresh chat without retrying the job',async()=>{
 const f=fixture({recoverSafeFailure:true,modelUnavailable:true,failedState:'failed'});
 try{await until(()=>f.w.document.querySelector('#story-chatgpt-bridge p').textContent.startsWith('Задание отклонено'));assert.equal(f.clicks,0);assert.equal(f.requests.filter(r=>r.path==='/claim').length,1);assert.equal(f.w.sessionStorage.getItem('story-chatgpt-probe-armed'),'yes');assert.equal(f.w.sessionStorage.getItem('story-chatgpt-probe-pending'),null);}finally{f.close();}
});
for(const options of [{recoverSafeFailure:true,wrongUser:true,failedState:'uncertain'},{recoverSafeFailure:true,modelUnavailable:true,failedState:'failed',previousSafeFailures:2}])test('uncertainty and repeated UI failures still pause '+JSON.stringify(options),async()=>{
 const f=fixture(options);try{await until(()=>f.w.sessionStorage.getItem('story-chatgpt-probe-armed')===null);assert.equal(f.requests.filter(r=>r.path==='/claim').length,1);assert.ok(!f.requests.some(r=>r.path==='/result'));}finally{f.close();}
});

for(const options of [{readable:true},{readable:true,rawEnvelope:true},{readable:true,truncatedMessage:true},{externalAnswer:true}])test('readable Markdown preserves exact task and supports scoped fallback '+JSON.stringify(options),async()=>{
 const f=fixture(options);try{await until(()=>f.requests.some(r=>r.path==='/result'));assert.equal(f.clicks,1);assert.ok(f.requests.find(r=>r.path==='/result').body.reply.includes('probe_id'));}finally{f.close();}
});
for(const options of [{readable:true,wrongUser:true},{readable:true,wrongMarker:true}])test('readable task correlation rejects different content or marker '+JSON.stringify(options),async()=>{
 const f=fixture(options);try{await until(()=>f.w.sessionStorage.getItem('story-chatgpt-probe-armed')===null);assert.equal(f.clicks,1);assert.ok(!f.requests.some(r=>r.path==='/result'));}finally{f.close();}
});
test('completed empty assistant UI terminates with bounded diagnostics and one send',async()=>{
 const f=fixture({emptyAnswer:true,rejectedResult:true});try{
  await until(()=>f.requests.some(r=>r.path==='/result'));
  const result=f.requests.find(r=>r.path==='/result').body;assert.equal(result.reply,'');assert.equal(f.clicks,1);
  const d=JSON.parse(result.error);assert.equal(d.stage,'completed-empty-ui');assert.equal(d.assistant.textLength,0);assert.equal(d.assistant.copyVisible,true);assert.ok(!result.error.includes('synthetic prompt'));
 }finally{f.close();}
});
for(const options of [{emptyAnswer:true,streamingEmpty:true},{emptyAnswer:true,userCopyOnly:true}])test('empty streaming UI or user-only copy does not claim completed output '+JSON.stringify(options),async()=>{
 const f=fixture(options);try{await until(()=>f.w.sessionStorage.getItem('story-chatgpt-probe-armed')===null);assert.ok(!f.requests.some(r=>r.path==='/result'));assert.equal(f.clicks,1);}finally{f.close();}
});

test('restore fragmented inline-code boundaries and require the full original envelope',async()=>{
 const f=fixture({readable:true,fragmentedInline:true});try{await until(()=>f.requests.some(r=>r.path==='/result'));assert.equal(f.clicks,1);}finally{f.close();}
});
test('restored Markdown mismatch terminates before the full answer timeout',async()=>{
 const f=fixture({readable:true,fragmentedInline:true,changedRestoredText:true});try{
  await until(()=>f.w.sessionStorage.getItem('story-chatgpt-probe-armed')===null);
  assert.ok(!f.requests.some(r=>r.path==='/result'));assert.equal(f.clicks,1);
  assert.ok(f.w.document.querySelector('#story-chatgpt-bridge p').textContent.includes('полный текст'));
  assert.ok(f.w.Date.now()<30000);
 }finally{f.close();}
});
test('an empty reply after restored full correlation is reported once',async()=>{
 const f=fixture({readable:true,fragmentedInline:true,emptyAnswer:true,rejectedResult:true});try{
  await until(()=>f.requests.some(r=>r.path==='/result'));const result=f.requests.find(r=>r.path==='/result').body;
  assert.equal(result.reply,'');assert.equal(JSON.parse(result.error).assistant.rawTextLength,0);assert.equal(f.clicks,1);
 }finally{f.close();}
});

for(const reasoningEffort of ['none','medium','high'])test('select and verify requested reasoning '+reasoningEffort,async()=>{
 const f=fixture({reasoningEffort,effortInitially:reasoningEffort==='medium'?2:0});try{await until(()=>f.requests.some(r=>r.path==='/result'));assert.equal(f.clicks,1);assert.equal(f.w.document.querySelector('button[aria-label="Выбрать модель ChatGPT"]').textContent,({none:'Instant',medium:'Medium',high:'High'})[reasoningEffort]);}finally{f.close();}
});
for(const options of [{reasoningEffort:'high',ignoreReasoning:true},{reasoningEffort:'medium',unknownSlider:true},{reasoningEffort:'unknown'}])test('unconfirmed reasoning blocks sending '+JSON.stringify(options),async()=>{
 const f=fixture(options);try{await until(()=>f.w.sessionStorage.getItem('story-chatgpt-probe-armed')===null);assert.equal(f.clicks,0);assert.ok(!f.requests.some(r=>r.path==='/send'));}finally{f.close();}
});

test('TXT: upload acknowledgement precedes one send and filename-correlated result',async()=>{
 const f=fixture({attachment:true});try{await until(()=>f.requests.some(r=>r.path==='/result'));assert.equal(f.clicks,1);assert.deepEqual(f.requests.filter(r=>r.path!=='/heartbeat').map(r=>r.path),['/claim','/attachment','/send','/result']);const ack=f.requests.find(r=>r.path==='/attachment').body;assert.equal(ack.fileName,'story-job-id.txt');assert.equal(ack.fileSha256.length,64);assert.ok(ack.fileBytes>0);}finally{f.close();}
});
for(const condition of ['badFileHash','missingFileInput','uploadPending','uploadError','uploadAckRejected'])test('TXT: no send on '+condition,async()=>{
 const f=fixture({attachment:true,[condition]:true});try{await until(()=>f.requests.some(r=>r.path==='/fail'));assert.equal(f.clicks,0);assert.equal(f.requests.filter(r=>r.path==='/send').length,0);}finally{f.close();}
});
test('TXT: removed attachment after authorization never clicks send',async()=>{
 const f=fixture({attachment:true,removeFileDuringAuthorization:true});try{await until(()=>f.requests.some(r=>r.path==='/fail'));assert.equal(f.clicks,0);assert.equal(f.requests.filter(r=>r.path==='/send').length,1);}finally{f.close();}
});
test('TXT: missing filename in sent turn cannot accept another answer',async()=>{
 const f=fixture({attachment:true,sentFileMissing:true});try{await until(()=>f.requests.some(r=>r.path==='/fail'));assert.equal(f.clicks,1);assert.equal(f.requests.filter(r=>r.path==='/result').length,0);}finally{f.close();}
});

for(const mode of ['autoAttachment','asyncAutoAttachment','fileDuringAuthorization'])test('Text: refuses an unexpected file '+mode,async()=>{
 const f=fixture({textOnly:true,[mode]:true});try{await until(()=>f.requests.some(r=>r.path==='/fail'));assert.equal(f.clicks,0);assert.equal(f.requests.filter(r=>r.path==='/result'||r.path==='/attachment').length,0);if(mode!=='fileDuringAuthorization')assert.equal(f.requests.filter(r=>r.path==='/send').length,0);}finally{f.close();}
});
test('Text: explicitly verifies no files without calling the attachment endpoint',async()=>{
 const f=fixture({textOnly:true,readable:true,rawEnvelope:true});try{await until(()=>f.requests.some(r=>r.path==='/result'));assert.equal(f.clicks,1);assert.equal(f.requests.find(r=>r.path==='/send').body.textOnlyVerified,true);assert.equal(f.requests.filter(r=>r.path==='/attachment').length,0);}finally{f.close();}
});
