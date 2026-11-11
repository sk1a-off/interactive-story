// ==UserScript==
// @name         Interactive Story ChatGPT Probe Worker
// @namespace    local.interactive-story
// @version      0.4.0
// @description  Story generation and probes, one fresh Temporary Chat per request.
// @match        https://chatgpt.com/*
// @grant        GM_xmlhttpRequest
// @grant        GM_getValue
// @grant        GM_setValue
// @grant        GM_registerMenuCommand
// @connect      127.0.0.1
// @run-at       document-idle
// ==/UserScript==
(() => {
  'use strict';
  const VERSION='0.4.0';
  const API='http://127.0.0.1:8791';
  const MARK='#story-bridge-worker';
  const FRESH='https://chatgpt.com/?temporary-chat=true'+MARK;
  const KEY='story-chatgpt-probe-';
  // Ignore every ordinary user tab. Never inspect account state or cookies.
  const registered=/^[a-f0-9]{8}-[a-f0-9-]{27}$/i.test(sessionStorage.getItem(KEY+'uuid')||'');
  if(location.hash!==MARK && !registered && sessionStorage.getItem(KEY+'armed')!=='yes')return;
  const worker=sessionStorage.getItem(KEY+'uuid')||crypto.randomUUID();
  sessionStorage.setItem(KEY+'uuid',worker);
  let token=String(GM_getValue(KEY+'token',''));
  let running=false;
  let lastModelFailure=null;
  let lastFailure=null;
  const panel=document.createElement('aside');panel.id='story-chatgpt-bridge';
  Object.assign(panel.style,{position:'fixed',right:'12px',top:'70px',zIndex:'2147483647',maxWidth:'330px',background:'#142238',color:'#edf3ff',padding:'14px',border:'1px solid #7ea1d2',borderRadius:'12px',font:'13px system-ui'});
  const title=document.createElement('strong');title.textContent='Мост '+VERSION+' · '+worker.slice(0,8);
  const status=document.createElement('p');
  const button=document.createElement('button');button.type='button';button.textContent='Запустить';
  Object.assign(button.style,{display:'inline-block',padding:'7px 12px',marginTop:'8px',background:'#315587',color:'#fff',border:'1px solid #8eb7ed',borderRadius:'7px',cursor:'pointer',pointerEvents:'auto'});
  const diagnosticButton=document.createElement('button');diagnosticButton.type='button';diagnosticButton.textContent='Диагностика';
  diagnosticButton.style.cssText=button.style.cssText;diagnosticButton.style.marginLeft='6px';
  diagnosticButton.onclick=async()=>{
    const report=JSON.stringify({version:VERSION,worker,running,lastModelFailure,lastFailure,current:modelSnapshot(),assistant:assistantState().diagnostic},null,2);
    try{await navigator.clipboard.writeText(report);show('Диагностика скопирована. Она содержит только состояние элементов управления, без текста чата и токенов.');}
    catch{show('Диагностика: '+report);}
  };
  panel.append(title,status,button,diagnosticButton);document.documentElement.append(panel);
  const show=text=>{status.textContent=text;};
  GM_registerMenuCommand('Токен локального моста',()=>{
    const value=window.prompt('Токен со страницы http://127.0.0.1:8791. Не пароль ChatGPT.','');
    if(value===null)return;
    if(!/^[a-f0-9]{48}$/.test(value.trim())){show('Неверный формат локального токена.');return;}
    GM_setValue(KEY+'token',value.trim());token=value.trim();show('Токен сохранён. Нажмите «Запустить».');
  });
  const pause=ms=>new Promise(resolve=>setTimeout(resolve,ms));
  function request(path,body){return new Promise((resolve,reject)=>{
    GM_xmlhttpRequest({method:'POST',url:API+path,headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json'},data:JSON.stringify({...body,version:VERSION}),timeout:10000,
      onload:r=>{if(r.status===204){resolve(null);return;}if(r.status<200||r.status>=300){reject(Error('Локальный мост: HTTP '+r.status));return;}try{resolve(JSON.parse(r.responseText));}catch{reject(Error('Неверный ответ локального моста'));}},
      onerror:()=>reject(Error('Нет связи с локальным мостом')),ontimeout:()=>reject(Error('Таймаут локального моста'))});
  });}
  // Selectors observed in the ChatGPT UI on 2026-11-10. Unknown layouts stop safely.
  const buttons=()=>[...document.querySelectorAll('button')].filter(e=>!panel.contains(e)&&e.getClientRects().length);
  // innerText includes the composer's opacity-zero animation placeholder.
  // Read rendered control text, skipping hidden animation/ARIA branches.
  function controlText(root){
    function read(node){
      if(node.nodeType===3)return node.textContent;
      if(node.nodeType!==1||node.matches('[hidden],[inert],[aria-hidden="true"]'))return '';
      const css=getComputedStyle(node);
      if(css.display==='none'||css.visibility==='hidden'||css.visibility==='collapse'||css.opacity==='0')return '';
      return [...node.childNodes].map(read).join('');
    }
    return root?read(root).replace(/\s+/g,' ').trim():'';
  }
  const label=e=>e.getAttribute('aria-label')||controlText(e);
  const exactButton=names=>buttons().find(e=>names.includes(label(e)));
  const roles=role=>[...document.querySelectorAll(`[data-conversation-role="${role}"]`)];
  function temporary(){
    return new URL(location.href).searchParams.get('temporary-chat')==='true' &&
      (!!exactButton(['Выйти из режима временного чата','Exit temporary chat']) ||
       /Временный чат|Temporary Chat/i.test(document.querySelector('main header')?.innerText||''));
  }
  function reasoningLabel(effort="none"){
    const picker=exactButton(['Выбрать модель ChatGPT','Choose ChatGPT model']);
    return picker&&controlText(picker)===({none:'Instant',medium:'Medium',high:'High'}[effort]);
  }
  function fileNameSeen(root,file,excluded=[]){
    if(!root||!file)return false;
    const stem=file.name.replace(/\.txt$/,'');
    return [...root.querySelectorAll('*')].some(e=>!panel.contains(e)&&!excluded.some(x=>x?.contains(e))&&e.getClientRects().length&&
      [e.getAttribute('title'),e.getAttribute('aria-label'),controlText(e)].some(text=>text===file.name||text===stem));
  }
  function attachmentState(file){
    const box=editor(),root=box?.closest('form')||document.querySelector('main');
    const pending=!!root?.querySelector('[role="progressbar"],[aria-busy="true"]');
    const alert=[...root?.querySelectorAll('[role="alert"]')||[]].filter(e=>e.getClientRects().length&&controlText(e));
    const remove=[...root?.querySelectorAll('button')||[]].filter(e=>/^(Remove file|Remove attachment|Удалить файл|Удалить вложение)/i.test(label(e)));
    return {present:fileNameSeen(root,file,[box]),pending,error:alert.length>0,extra:remove.length>1};
  }
  async function attachText(file,lease){
    const bytes=new TextEncoder().encode(file.content);
    const upload=new File([bytes],file.name,{type:'text/plain;charset=utf-8'});
    const digest=await crypto.subtle.digest('SHA-256',await upload.arrayBuffer());
    const hash=[...new Uint8Array(digest)].map(x=>x.toString(16).padStart(2,'0')).join('');
    if(upload.size!==file.bytes||hash!==file.sha256)throw Error('TXT: размер или SHA-256 не совпадает с заданием.');
    const inputs=[...document.querySelectorAll('input[type="file"]')];
    const input=inputs.find(e=>!e.accept||['*','*/*'].includes(e.accept)||/text\/plain|\.txt/i.test(e.accept));
    if(!input)throw Error('TXT: не найдено поле загрузки файлов.');
    const transfer=new DataTransfer();transfer.items.add(upload);
    input.files=transfer.files;input.dispatchEvent(new Event('change',{bubbles:true}));
    let since=0;
    await waitFor(()=>{
      const state=attachmentState(file);
      if(state.error||state.extra)throw Error('TXT: ошибка загрузки или постороннее вложение.');
      const send=exactButton(['Отправить','Send']);
      if(!state.present||state.pending||!send||send.disabled){since=0;return false;}
      if(!since)since=Date.now();return Date.now()-since>=1500;
    },60000);
    const accepted=await request('/attachment',{...lease,fileName:file.name,fileSha256:hash,fileBytes:upload.size});
    if(!accepted.verified)throw Error('TXT: мост не подтвердил вложение.');
  }
  function userMessage(current){
    const heading=roles('user')[0];
    const bubble=heading?.parentElement.querySelector('[data-user-message-bubble]');
    const contents=[...bubble?.querySelectorAll('[data-search-result-target]')||[]].filter(e=>{const parent=e.parentElement.closest('[data-search-result-target]');return (!parent||!bubble.contains(parent))&&e.getClientRects().length;});
    const more=[...bubble?.querySelectorAll('button')||[]].find(e=>['Показать ещё','Показать больше','Show more'].includes(controlText(e)));
    if(current?.messageFormat==='attachment-txt-v1'&&bubble){
      const raw=contents.map(e=>e.textContent).join('').trim();
      const fileSeen=fileNameSeen(heading.parentElement,current.attachment,contents);
      if(raw===current.prompt&&fileSeen)return {text:current.prompt,source:'attached-txt',bubble};
      return {text:null,source:fileSeen?'attachment-text-pending':'attachment-missing',bubble,invalid:!!raw&&raw!==current.prompt};
    }
    if(current?.messageFormat==='readable-json-v3'&&bubble){
      const codes=[...bubble.querySelectorAll('code')];
      const markerSeen=bubble.textContent.includes(current.messageMarker);
      const raw=contents.map(e=>e.textContent).join('').trim();
      if(raw===current.prompt)return {text:current.messageContent,source:'raw-envelope',bubble,more};
      // The live renderer pairs single backticks even inside a double-backtick
      // span. Restore only observed CODE boundaries, then require the ENTIRE
      // original envelope including the unique marker. Never strip punctuation.
      function restore(node){
        if(node.nodeType===3)return node.textContent;
        if(node.nodeType!==1)return '';
        if(node.tagName==='CODE')return '`'+node.textContent+'`';
        return [...node.childNodes].map(restore).join('');
      }
      const restored=contents.map(restore).join('').trim();
      if(restored===current.prompt)return {text:current.messageContent,source:'restored-inline-code',bubble,more};
      if(markerSeen&&codes.length===1){
        const text=codes[0].textContent.trim();
        if(current.messageContent.startsWith(text))return {text,source:'fenced-block',bubble,more};
      }
      return {text:null,source:markerSeen?'block-not-mounted':'marker-missing',bubble,more,invalid:!markerSeen&&!!raw};
    }
    // Long messages add a Show more button inside the bubble. It is not part
    // of the sent prompt; reading the entire bubble breaks strict correlation.
    if(contents.length)return {text:contents.map(e=>e.textContent).join('').trim(),source:'data-search-result-target',bubble,more:[...bubble.querySelectorAll('button')].find(e=>['Показать ещё','Показать больше','Show more'].includes(controlText(e)))};
    if(bubble&&!bubble.querySelector('button,[role="button"]'))return {text:bubble.textContent.trim(),source:'plain-bubble'};
    return {text:null,source:'unrecognized'};
  }
  const normalized=text=>String(text??'').replace(/\r\n?/g,'\n').trim();
  function editorMatches(box,prompt){return [box.textContent,box.innerText].some(text=>normalized(text)===normalized(prompt));}
  function assistantState(){
    const heading=roles('assistant')[0];
    const scoped=heading?.parentElement.querySelectorAll('[data-markdown-text-style="assistant-message"]')||[];
    const global=[...document.querySelectorAll('[data-markdown-text-style="assistant-message"]')];
    const root=scoped.length===1?scoped[0]:roles('assistant').length===1&&global.length===1?global[0]:null;
    const text=root?(root.innerText??root.textContent??'').trim():'';
    const stopped=!!exactButton(['Остановить','Stop','Stop generating']);
    const copy=!!heading&&buttons().some(e=>['Копировать','Copy'].includes(label(e))&&!!(heading.compareDocumentPosition(e)&Node.DOCUMENT_POSITION_FOLLOWING));
    return {text,diagnostic:{assistantCount:roles('assistant').length,scopedRoots:scoped.length,globalRoots:global.length,textLength:text.length,rawTextLength:root?.textContent.length||0,codeBlocks:root?.querySelectorAll('pre code').length||0,stopVisible:stopped,copyVisible:copy}};
  }
  function modelSnapshot(){
    const picker=exactButton(['Выбрать модель ChatGPT','Choose ChatGPT model']);
    return {
      temporary:temporary(),
      picker:picker?{text:controlText(picker),expanded:picker.getAttribute('aria-expanded'),state:picker.getAttribute('data-state'),effort:picker.getAttribute('data-selected-reasoning-effort')}:null,
      menus:[...document.querySelectorAll('[role="menu"]')].filter(e=>e.querySelector('[data-model-picker-view-toggle],[data-reasoning-slider]')).map(e=>({state:e.getAttribute('data-state'),hidden:e.getAttribute('aria-hidden'),inert:!!e.closest('[inert]')})),
      sliders:[...document.querySelectorAll('[data-model-picker-power-slider] [role="slider"]')].map(e=>({min:e.getAttribute('aria-valuemin'),value:e.getAttribute('aria-valuenow'),hidden:e.getAttribute('aria-hidden')})),
    };
  }
  function pointerClick(element){
    element.focus();
    element.dispatchEvent(new PointerEvent('pointerdown',{bubbles:true,composed:true,pointerType:'mouse',pointerId:1,isPrimary:true,button:0,buttons:1}));
    element.dispatchEvent(new MouseEvent('mousedown',{bubbles:true,composed:true,button:0,buttons:1}));
    element.dispatchEvent(new PointerEvent('pointerup',{bubbles:true,composed:true,pointerType:'mouse',pointerId:1,isPrimary:true,button:0,buttons:0}));
    element.dispatchEvent(new MouseEvent('mouseup',{bubbles:true,composed:true,button:0,buttons:0}));
    element.click();
  }
  async function selectGPT6(effort="none"){
    const targetMode={none:{index:0,label:'Instant'},medium:{index:1,label:'Medium'},high:{index:2,label:'High'}}[effort];
    if(!targetMode)throw Error('Неизвестная сила рассуждения');
    const getPicker=()=>exactButton(['Выбрать модель ChatGPT','Choose ChatGPT model']);
    const visible=selector=>[...document.querySelectorAll(selector)].find(e=>!e.closest('[inert],[aria-hidden="true"]'));
    const getToggle=()=>visible('[data-model-picker-view-toggle]');
    const getModel=()=>[...document.querySelectorAll('[role="menuitemradio"]')].find(e=>!e.closest('[inert],[aria-hidden="true"]')&&controlText(e)==='GPT-6'&&e.getAttribute('aria-disabled')!=='true');
    const activeMenu=()=>[...document.querySelectorAll('[role="menu"]')].find(e=>e.getAttribute('data-state')!=='closed'&&!e.closest('[inert],[aria-hidden="true"]')&&e.querySelector('[data-model-picker-view-toggle],[data-reasoning-slider]'));
    const menuClosed=()=>getPicker()?.getAttribute('aria-expanded')!=='true'&&!activeMenu();
    async function closeMenu(){
      if(menuClosed())return;
      // Radix-style triggers can react to pointerdown rather than .click().
      // Escape is the normal dismissal path; do not toggle the trigger again.
      const target=activeMenu()||document.activeElement||getPicker();
      for(const type of ['keydown','keyup'])target?.dispatchEvent(new KeyboardEvent(type,{key:'Escape',code:'Escape',keyCode:27,bubbles:true,composed:true}));
      await pause(200);
      if(!menuClosed()){
        const box=editor();
        if(box){
          box.dispatchEvent(new PointerEvent('pointerdown',{bubbles:true,composed:true,pointerType:'mouse',button:0}));
          box.dispatchEvent(new MouseEvent('mousedown',{bubbles:true,composed:true,button:0}));
          box.dispatchEvent(new PointerEvent('pointerup',{bubbles:true,composed:true,pointerType:'mouse',button:0}));
          box.dispatchEvent(new MouseEvent('mouseup',{bubbles:true,composed:true,button:0}));
          box.click();box.focus();
        }
      }
      await waitFor(menuClosed,3000);
    }
    let stage='открытие меню модели';
    try{
      const picker=await waitFor(getPicker,15000);
      // A previous failure or the user may have left the menu open.
      stage='подтверждение открытия меню модели';
      // A freshly loaded page can render its controls before event handlers
      // are ready. ArrowDown requests opening (not a blind toggle).
      for(let attempt=0;attempt<8;attempt++){
        const trigger=getPicker();
        if(trigger?.getAttribute('aria-expanded')==='true'&&activeMenu())break;
        if(trigger&&!trigger.disabled&&trigger.getAttribute('aria-expanded')!=='true'){
          trigger.focus();
          for(const type of ['keydown','keyup'])trigger.dispatchEvent(new KeyboardEvent(type,{key:'ArrowDown',code:'ArrowDown',keyCode:40,bubbles:true,composed:true}));
          if(attempt===3&&trigger.getAttribute('aria-expanded')!=='true')pointerClick(trigger);
        }
        await pause(400);
      }
      await waitFor(()=>getPicker()?.getAttribute('aria-expanded')==='true'&&activeMenu(),1500);
      stage='открытие списка моделей';
      (await waitFor(getToggle,5000)).click();
      stage='выбор GPT-6';
      let model=await waitFor(getModel,5000);
      const alreadySelected=model.getAttribute('aria-checked')==='true';
      model.click();
      if(!alreadySelected){
        // Verify while the model list is visible. Other menu views can unmount it.
        (await waitFor(getToggle,5000)).click();
        stage='подтверждение выбранной GPT-6';
        model=await waitFor(()=>{const e=getModel();return e?.getAttribute('aria-checked')==='true'&&e;},5000);
        model.click();
      }
      stage='установка мощности '+targetMode.label;
      const power=await waitFor(()=>visible('[data-reasoning-slider="true"]'),5000);
      const slider=()=>[...document.querySelectorAll('[data-model-picker-power-slider] [role="slider"]')].find(e=>!e.closest('[inert]')&&!e.parentElement.closest('[aria-hidden="true"]'));
      const selected=()=>{const e=slider();return e&&Number(e.getAttribute('aria-valuenow'))===targetMode.index&&e.getAttribute('aria-valuemin')==='0'&&e.getAttribute('aria-valuemax')==='2';};
      power.focus();
      for(let i=0;i<5&&!selected();i++){
        const current=slider();if(!current)throw Error('Не найдена шкала рассуждения');
        const key=Number(current.getAttribute('aria-valuenow'))<targetMode.index?'ArrowRight':'ArrowLeft';
        for(const type of ['keydown','keyup'])power.dispatchEvent(new KeyboardEvent(type,{key,code:key,bubbles:true}));
        await pause(150);
      }
      await waitFor(()=>selected()&&controlText(getToggle())===targetMode.label,5000);
      stage='закрытие меню модели';
      await closeMenu();
      await waitFor(()=>reasoningLabel(effort),3000);
    }catch(e){
      lastModelFailure={stage,...modelSnapshot()};
      try{await closeMenu();}catch{/* Preserve the original failed stage; never send. */}
      throw Error('GPT-6: '+stage+'. '+(e.message||e));
    }
  }
  function editor(){return document.querySelector('[contenteditable="true"][role="textbox"]');}
  function empty(){return temporary()&&location.pathname==='/'&&roles('user').length===0&&roles('assistant').length===0&&editor()&&editor().textContent.trim()==='';}
  async function waitFor(check,timeout){
    const end=Date.now()+timeout;
    while(running&&Date.now()<end){const value=check();if(value)return value;await pause(500);}
    throw Error(running?'Страница не готова или ответ не завершён':'Рабочий остановлен');
  }
  function stop(message){void request('/heartbeat',{worker,status:'attention'}).catch(()=>{});running=false;sessionStorage.removeItem(KEY+'armed');button.textContent='Запустить';show(message);}
  button.onclick=()=>{
    if(running){stop('Остановлен. Текущий запрос автоматически не повторяется.');void request('/heartbeat',{worker,status:'stopped'}).catch(()=>{});return;}
    if(!token){show('Задайте токен через меню userscript.');return;}
    sessionStorage.setItem(KEY+'armed','yes');
    // Assigning the already open URL (or changing only its fragment) does not
    // reload the page. Start here when it is already a fresh Temporary Chat.
    if(empty()){void work();return;}
    const target=new URL(FRESH);
    if(location.origin===target.origin&&location.pathname===target.pathname&&location.search===target.search){
      location.reload();
    }else{location.assign(FRESH);}
  };
  async function work(){
    running=true;button.textContent='Остановить';show('Проверка нового временного чата…');
    let current=null,knownTerminal=null;
    lastModelFailure=null;lastFailure=null;
    try{
      // A reload/crash during a job never resumes sending, even if the click was uncertain.
      const interrupted=sessionStorage.getItem(KEY+'pending');
      if(interrupted){
        sessionStorage.removeItem(KEY+'pending');
        try{await request('/fail',JSON.parse(interrupted));}catch{/* Server deadline also terminalizes it. */}
        throw Error('Предыдущий запрос прерван. Проверьте его статус; повтора не будет.');
      }
      await waitFor(empty,45000);
      while(running){
        if(!empty())throw Error('Нужен пустой подтверждённый временный чат.');
        current=await request('/claim',{worker});
        if(!current){show('Готов. Ожидание задания…');await pause(1500);continue;}
        const lease={worker,id:current.id,lease:current.lease};
        sessionStorage.setItem(KEY+'pending',JSON.stringify(lease));
        show('Выбор GPT-6 · '+(current.reasoningEffort||'none')+' · '+current.id.slice(0,8));
        await selectGPT6(current.reasoningEffort||"none");
        if(!running||!empty())throw Error('Страница изменилась до отправки.');
        const box=editor();box.focus();
        // Use the browser editing command so the React composer receives a native input event.
        if(!document.execCommand('insertText',false,current.prompt))throw Error('Не удалось заполнить поле.');
        if(!editorMatches(box,current.prompt))throw Error('Текст поля не совпадает с заданием.');
        if(current.attachment){show('Загружаю TXT · '+current.attachment.bytes+' байт…');await attachText(current.attachment,lease);}
        const send=await waitFor(()=>{const e=exactButton(['Отправить','Send']);return e&&!e.disabled&&e;},5000);
        if(!running||!temporary()||!reasoningLabel(current.reasoningEffort||"none")||roles('user').length||roles('assistant').length)throw Error('Проверка перед отправкой не пройдена.');
        show('GPT-6 · '+(current.reasoningEffort||'none')+' · '+current.id.slice(0,8)+' · ожидание ответа…');
        await request('/send',lease); // A failed/ambiguous authorization NEVER leads to a click or retry.
        if(!running||!temporary()||!reasoningLabel(current.reasoningEffort||"none")||!editorMatches(box,current.prompt))throw Error('Отправка остановлена после разрешения.');
        if(current.attachment){const state=attachmentState(current.attachment);if(!state.present||state.pending||state.error||state.extra)throw Error('TXT исчез или изменился перед отправкой.');}
        send.click(); // Exactly one click for this lease.
        let stable='',since=0,emptySince=0,correlationSince=0,expandedBubble=null,emptyReply=false;
        const reply=await waitFor(()=>{
          if(!temporary())throw Error('Временный режим больше не подтверждён.');
          const users=roles('user'),assistants=roles('assistant');
          if(users.length>1||assistants.length>1)throw Error('Неожиданный дополнительный ход чата.');
          const shown=userMessage(current),userText=shown.text;
          const expected=current.messageFormat==='readable-json-v3'?current.messageContent:current.prompt;
          if(users.length===1&&userText!==expected){
            show('Проверяю полный текст отправленного запроса…');
            if(!correlationSince)correlationSince=Date.now();
            if(Date.now()-correlationSince>=4000)throw Error('Не удалось подтвердить полный текст отправленного запроса.');
          }else{correlationSince=0;}
          if(shown.invalid)throw Error('Ответ принадлежит другому запросу.');
          if(userText&&userText!==expected){
            if(expected.startsWith(userText)&&userText.length<expected.length){
              if(shown.more&&expandedBubble!==shown.bubble){expandedBubble=shown.bubble;shown.more.click();}
              return null;
            }
            throw Error('Ответ принадлежит другому запросу.');
          }
          if(!userText&&shown.more&&expandedBubble!==shown.bubble){expandedBubble=shown.bubble;shown.more.click();return null;}
          const state=assistantState(),text=state.text,d=state.diagnostic;
          const done=users.length===1&&userText===expected&&assistants.length===1&&!d.stopVisible&&d.copyVisible;
          if(done&&!text){
            show('Ответ завершён без читаемого текста. Проверяю…');
            if(!emptySince)emptySince=Date.now();
            // A completed UI with no readable text is a terminal observation,
            // not proof that the underlying model generated no content.
            if(Date.now()-emptySince>=5000){emptyReply=true;return {text:''};}
          }else{
            emptySince=0;
            show(d.stopVisible?(text?'Получаю ответ · '+text.length+' символов…':'Модель ещё генерирует ответ…'):text?'Ответ получен. Проверяю завершение…':'Ожидание ответа модели…');
          }
          if(!done||!text){stable='';since=0;return null;}
          if(text!==stable){stable=text;since=Date.now();return null;}
          return Date.now()-since>=2500 ? {text}:null;
        },Math.min(570000,Math.max(130000,current.responseTimeoutMs||130000)));
        const accepted=await request('/result',{...lease,reply:reply.text,error:emptyReply?JSON.stringify({version:VERSION,stage:'completed-empty-ui',message:'UI completed without extractable text',assistant:assistantState().diagnostic}):undefined});
        knownTerminal=accepted.state;
        if(accepted.state!=='complete')throw Error(emptyReply?'Ответ завершён в интерфейсе, но читаемый текст отсутствует.':'Мост отклонил ответ: неверный JSON или идентификатор.');
        sessionStorage.removeItem(KEY+'pending');sessionStorage.removeItem(KEY+'safeFailures');current=null;
        if(running){show('Ответ получен. Открываю новый временный чат…');location.replace(FRESH);}return;
      }
    }catch(e){
      const shown=userMessage(current);
      const expected=current?.messageFormat==='readable-json-v3'?current.messageContent:current?.prompt;
      lastFailure={message:String(e.message||e),temporary:temporary(),fileInputs:[...document.querySelectorAll('input[type="file"]')].map(e=>({accept:e.accept,multiple:e.multiple})),attachment:current?.attachment?attachmentState(current.attachment):null,userMessage:{source:shown.source,expectedLength:expected?.length??null,actualLength:shown.text?.length??null,matches:current?shown.text===expected:null}};
      if(current){try{const failed=await request('/fail',{worker,id:current.id,lease:current.lease,error:JSON.stringify({version:VERSION,stage:lastModelFailure?.stage||'transport',message:lastFailure.message,expectedLength:lastFailure.userMessage.expectedLength,actualLength:lastFailure.userMessage.actualLength,assistant:assistantState().diagnostic})});knownTerminal=failed.state;}catch{/* No resubmission. */}}
      if(running&&current?.recoverSafeFailure&&knownTerminal==='failed'){
        const failures=Number(sessionStorage.getItem(KEY+'safeFailures')||0)+1;
        sessionStorage.setItem(KEY+'safeFailures',String(failures));
        if(failures<3){sessionStorage.removeItem(KEY+'pending');show('Задание отклонено и не повторяется. Открываю новый чат для следующего задания.');location.replace(FRESH);return;}
      }
      sessionStorage.removeItem(KEY+'pending');stop(String(e.message||e));
    }
  }
  setInterval(()=>{if(running)void request('/heartbeat',{worker,status:sessionStorage.getItem(KEY+'pending')?'busy':'ready'}).catch(()=>show('Нет связи с мостом. Отправка не повторяется.'));},5000);
  if(sessionStorage.getItem(KEY+'armed')==='yes')void work();else show('Отдельная рабочая вкладка. Нажмите «Запустить», чтобы разрешить тесты.');
})();
