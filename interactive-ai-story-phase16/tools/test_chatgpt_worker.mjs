// DOM contract tests, not a substitute for a live userscript-manager test.
import {readFileSync} from 'node:fs';
import {createRequire} from 'node:module';
import {test} from 'node:test';
import assert from 'node:assert/strict';
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
 Object.defineProperty(w.HTMLElement.prototype,'innerText',{get(){return this.textContent;},set(value){this.textContent=value;}});
 const j={id:'job-id',worker:'test-worker',lease:'lease-secret',prompt:'synthetic prompt'};
 const box=w.document.querySelector('[contenteditable]');
 w.document.querySelector('button[aria-haspopup="menu"]').onclick=()=>{personalizationClicks++;};
 const picker=w.document.querySelector('button[aria-label="Выбрать модель ChatGPT"]');
 let selectedModel=options.modelAlreadySelected?'GPT-6':'GPT-5.6 Sol';
 let effort=options.instantInitially?0:1;
 function renderSimple(menu){
  menu.replaceChildren();
  const toggle=w.document.createElement('div');toggle.dataset.modelPickerViewToggle='true';toggle.textContent=effort===0?'Instant':'Medium';menu.append(toggle);
  toggle.onclick=()=>{
   menu.replaceChildren();
   if(options.modelUnavailable)return;
   const model=w.document.createElement('div');model.setAttribute('role','menuitemradio');model.setAttribute('aria-checked',String(selectedModel==='GPT-6'));model.textContent='GPT-6';menu.append(model);
   model.onclick=()=>{if(!options.modelSelectionIgnored)selectedModel='GPT-6';renderSimple(menu);};
  };
  const power=w.document.createElement('div');power.dataset.reasoningSlider='true';
  const track=w.document.createElement('div');track.dataset.modelPickerPowerSlider='';
  const slider=w.document.createElement('span');slider.setAttribute('role','slider');slider.setAttribute('aria-hidden','true');slider.setAttribute('aria-valuemin','0');slider.setAttribute('aria-valuenow',String(effort));track.append(slider);
  const status=w.document.createElement('span');status.setAttribute('role','status');status.textContent=effort===0?'Instant, 1 из 3.':'Medium, 2 из 3.';
  power.onkeydown=e=>{if(e.key==='ArrowLeft'){effort=0;slider.setAttribute('aria-valuenow','0');status.textContent='Instant, 1 из 3.';picker.textContent='Instant';toggle.textContent='Instant';}};
  menu.append(power,track);if(!options.noAnnouncement)menu.append(status);
 }
 picker.onclick=()=>{
  const existing=w.document.querySelector('[role="menu"]');
  if(existing){existing.remove();picker.setAttribute('aria-expanded','false');return;}
  picker.setAttribute('aria-expanded','true');
  const menu=w.document.createElement('div');menu.setAttribute('role','menu');renderSimple(menu);w.document.body.append(menu);
 };
 if(effort===0)picker.textContent='Instant';
 if(options.menuInitiallyOpen)picker.click();
 w.document.execCommand=(_cmd,_ui,text)=>{box.textContent=text;return true;};
 w.GM_getValue=()=> 'a'.repeat(48);w.GM_setValue=()=>{};w.GM_registerMenuCommand=()=>{};
 w.GM_xmlhttpRequest=r=>{
  const path=new URL(r.url).pathname,body=JSON.parse(r.data);requests.push({path,body});
  if(path==='/send'&&options.lostAuthorization){queueMicrotask(()=>r.ontimeout());return;}
  if(path==='/send'&&options.stopDuringAuthorization)w.document.querySelector('#story-chatgpt-bridge button').click();
  let value={};if(path==='/claim')value=j;
  if(path==='/result')value={state:options.rejectedResult?'failed':'complete'};
  queueMicrotask(()=>r.onload({status:200,responseText:JSON.stringify(value)}));
 };
 w.document.querySelector('button[aria-label="Отправить"]').onclick=()=>{
  clicks++;
  const user=w.document.createElement('div');const heading=w.document.createElement('h4');heading.dataset.conversationRole='user';
  const bubble=w.document.createElement('div');bubble.dataset.userMessageBubble='true';bubble.textContent=options.wrongUser?'unrelated prompt':j.prompt;user.append(heading,bubble);
  const assistant=w.document.createElement('div');const h=w.document.createElement('h4');h.dataset.conversationRole='assistant';
  const answer=w.document.createElement('div');answer.dataset.markdownTextStyle='assistant-message';answer.textContent='{"probe_id":"job-id","ok":true}';assistant.append(h,answer);
  const copy=w.document.createElement('button');copy.setAttribute('aria-label','Копировать');
  w.document.querySelector('#transcript').append(user,assistant,copy);
 };
 if(!options.unarmed)w.sessionStorage.setItem('story-chatgpt-probe-armed','yes');
 if(options.pending)w.sessionStorage.setItem('story-chatgpt-probe-pending',JSON.stringify({worker:j.worker,id:j.id,lease:j.lease}));
 if(options.personalized)w.document.querySelector('button[aria-haspopup="menu"]').setAttribute('aria-label','Персонализированный');
 if(options.noPersonalizationControl)w.document.querySelector('button[aria-haspopup="menu"]').remove();
 if(options.notTemporary){w.document.querySelector('header').textContent='Regular chat';w.document.querySelector('button').remove();}
 w.eval(source);
 return {w,requests,get clicks(){return clicks;},get personalizationClicks(){return personalizationClicks;},close:()=>dom.window.close()};
}
test('one send and completed reply correlated to current lease',async()=>{
 const f=fixture({personalized:true});try{await until(()=>f.requests.some(r=>r.path==='/result')).catch(e=>{throw Error(e.message+' '+f.w.document.querySelector('#story-chatgpt-bridge').textContent+' '+JSON.stringify(f.requests));});assert.equal(f.clicks,1);assert.deepEqual(f.requests.filter(r=>r.path!=='/heartbeat').map(r=>r.path),['/claim','/send','/result']);const body=f.requests.at(-1).body;assert.equal(body.id,'job-id');assert.equal(body.lease,'lease-secret');}finally{f.close();}
});
for(const options of [{lostAuthorization:true},{stopDuringAuthorization:true},{wrongUser:true},{rejectedResult:true},{pending:true},{notTemporary:true},{modelUnavailable:true},{modelSelectionIgnored:true}])test('safe stop '+JSON.stringify(options),async()=>{
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
