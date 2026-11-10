// ==UserScript==
// @name         Interactive Story ChatGPT Probe Worker
// @namespace    local.interactive-story
// @version      0.1.0
// @description  Opt-in synthetic probes, one fresh Temporary Chat per request.
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
  const API='http://127.0.0.1:8791';
  const MARK='#story-bridge-worker';
  const FRESH='https://chatgpt.com/?temporary-chat=true'+MARK;
  const KEY='story-chatgpt-probe-';
  // Ignore every ordinary user tab. Never inspect account state or cookies.
  if(location.hash!==MARK && sessionStorage.getItem(KEY+'armed')!=='yes')return;
  const worker=sessionStorage.getItem(KEY+'uuid')||crypto.randomUUID();
  sessionStorage.setItem(KEY+'uuid',worker);
  let token=String(GM_getValue(KEY+'token',''));
  let running=false;
  const panel=document.createElement('aside');panel.id='story-chatgpt-bridge';
  Object.assign(panel.style,{position:'fixed',right:'12px',top:'70px',zIndex:'2147483647',maxWidth:'330px',background:'#142238',color:'#edf3ff',padding:'14px',border:'1px solid #7ea1d2',borderRadius:'12px',font:'13px system-ui'});
  const title=document.createElement('strong');title.textContent='Мост · '+worker.slice(0,8)+' · только тесты';
  const status=document.createElement('p');
  const button=document.createElement('button');button.textContent='Запустить';
  panel.append(title,status,button);document.documentElement.append(panel);
  const show=text=>{status.textContent=text;};
  GM_registerMenuCommand('Токен локального моста',()=>{
    const value=window.prompt('Токен со страницы http://127.0.0.1:8791. Не пароль ChatGPT.','');
    if(value===null)return;
    if(!/^[a-f0-9]{48}$/.test(value.trim())){show('Неверный формат локального токена.');return;}
    GM_setValue(KEY+'token',value.trim());token=value.trim();show('Токен сохранён. Нажмите «Запустить».');
  });
  const pause=ms=>new Promise(resolve=>setTimeout(resolve,ms));
  function request(path,body){return new Promise((resolve,reject)=>{
    GM_xmlhttpRequest({method:'POST',url:API+path,headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json'},data:JSON.stringify(body),timeout:10000,
      onload:r=>{if(r.status===204){resolve(null);return;}if(r.status<200||r.status>=300){reject(Error('Локальный мост: HTTP '+r.status));return;}try{resolve(JSON.parse(r.responseText));}catch{reject(Error('Неверный ответ локального моста'));}},
      onerror:()=>reject(Error('Нет связи с локальным мостом')),ontimeout:()=>reject(Error('Таймаут локального моста'))});
  });}
  // Selectors observed in the ChatGPT UI on 2026-11-10. Unknown layouts stop safely.
  const buttons=()=>[...document.querySelectorAll('button')].filter(e=>!panel.contains(e)&&e.getClientRects().length);
  const label=e=>e.getAttribute('aria-label')||e.textContent.trim();
  const exactButton=names=>buttons().find(e=>names.includes(label(e)));
  const roles=role=>[...document.querySelectorAll(`[data-conversation-role="${role}"]`)];
  function temporary(){
    return new URL(location.href).searchParams.get('temporary-chat')==='true' &&
      (!!exactButton(['Выйти из режима временного чата','Exit temporary chat']) ||
       /Временный чат|Temporary Chat/i.test(document.querySelector('main header')?.innerText||''));
  }
  function noPersonalization(){return !!exactButton(['Без персонализации','Unpersonalized','Not personalized']);}
  async function disablePersonalization(){
    const toggle=await waitFor(()=>exactButton(['Без персонализации','Персонализированный','Unpersonalized','Not personalized','Personalized']),15000);
    toggle.click();
    const choice=await waitFor(()=>[...document.querySelectorAll('[role="menuitemradio"]')].find(e=>/^(Без персонализации|Unpersonalized|Not personalized)(\n|$)/i.test(e.innerText.trim())),5000);
    // Select explicitly; the composer label must confirm the result.
    choice.click();
    await waitFor(noPersonalization,5000);
  }
  function instantLabel(){
    const picker=exactButton(['Выбрать модель ChatGPT','Choose ChatGPT model']);
    return picker&&/^Instant$/.test(picker.innerText.trim());
  }
  async function selectGPT6Instant(){
    const picker=await waitFor(()=>exactButton(['Выбрать модель ChatGPT','Choose ChatGPT model']),15000);
    picker.click();
    const toggle=await waitFor(()=>[...document.querySelectorAll('[data-model-picker-view-toggle]')].find(e=>!e.closest('[inert]')),5000);
    toggle.click();
    const model=await waitFor(()=>[...document.querySelectorAll('[role="menuitemradio"]')].find(e=>!e.closest('[inert]')&&e.innerText.trim()==='GPT-6'&&e.getAttribute('aria-disabled')!=='true'),5000);
    model.click();
    const power=await waitFor(()=>[...document.querySelectorAll('[data-reasoning-slider="true"]')].find(e=>!e.closest('[inert]')),5000);
    power.focus();
    for(let i=0;i<3;i++){
      power.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowLeft',code:'ArrowLeft',bubbles:true}));
      power.dispatchEvent(new KeyboardEvent('keyup',{key:'ArrowLeft',code:'ArrowLeft',bubbles:true}));
      await pause(100);
    }
    await waitFor(()=>[...document.querySelectorAll('[role="menuitemradio"][aria-checked="true"]')].some(e=>e.innerText.trim()==='GPT-6')&&
      document.querySelector('[data-model-picker-power-slider] [role="slider"]')?.getAttribute('aria-valuenow')==='0'&&
      [...document.querySelectorAll('[role="menu"] [role="status"]')].some(e=>/^Instant,?\s/.test(e.textContent.trim())),5000);
    picker.click();
    await waitFor(()=>picker.getAttribute('aria-expanded')==='false'&&instantLabel(),5000);
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
    sessionStorage.setItem(KEY+'armed','yes');location.assign(FRESH);
  };
  async function work(){
    running=true;button.textContent='Остановить';show('Проверка нового временного чата…');
    let current=null;
    try{
      // A reload/crash during a job never resumes sending, even if the click was uncertain.
      const interrupted=sessionStorage.getItem(KEY+'pending');
      if(interrupted){
        sessionStorage.removeItem(KEY+'pending');
        try{await request('/fail',JSON.parse(interrupted));}catch{/* Server deadline also terminalizes it. */}
        throw Error('Предыдущий запрос прерван. Проверьте его статус; повтора не будет.');
      }
      await waitFor(empty,45000);
      await disablePersonalization().catch(()=>{throw Error('Не удалось включить режим «Без персонализации». Проверьте рабочую вкладку.');});
      while(running){
        if(!empty()||!noPersonalization())throw Error('Нужен пустой временный чат без персонализации.');
        current=await request('/claim',{worker});
        if(!current){show('Готов. Ожидание синтетического задания…');await pause(1500);continue;}
        const lease={worker,id:current.id,lease:current.lease};
        sessionStorage.setItem(KEY+'pending',JSON.stringify(lease));
        show('Выбор GPT-6 Instant · '+current.id.slice(0,8));
        await selectGPT6Instant().catch(()=>{throw Error('Не удалось подтвердить GPT-6 Instant. Проверьте доступность модели и рабочую вкладку.');});
        if(!running||!empty()||!noPersonalization())throw Error('Страница изменилась до отправки.');
        const box=editor();box.focus();
        // Use the browser editing command so the React composer receives a native input event.
        if(!document.execCommand('insertText',false,current.prompt))throw Error('Не удалось заполнить поле.');
        if(box.textContent.trim()!==current.prompt)throw Error('Текст поля не совпадает с заданием.');
        const send=await waitFor(()=>{const e=exactButton(['Отправить','Send']);return e&&!e.disabled&&e;},5000);
        if(!running||!temporary()||!noPersonalization()||!instantLabel()||roles('user').length||roles('assistant').length)throw Error('Проверка перед отправкой не пройдена.');
        show('GPT-6 Instant · '+current.id.slice(0,8)+' · ожидание ответа…');
        await request('/send',lease); // A failed/ambiguous authorization NEVER leads to a click or retry.
        if(!running||!temporary()||!noPersonalization()||!instantLabel()||box.textContent.trim()!==current.prompt)throw Error('Отправка остановлена после разрешения.');
        send.click(); // Exactly one click for this lease.
        let stable='',since=0;
        const reply=await waitFor(()=>{
          if(!temporary())throw Error('Временный режим больше не подтверждён.');
          const users=roles('user'),assistants=roles('assistant');
          if(users.length>1||assistants.length>1)throw Error('Неожиданный дополнительный ход чата.');
          const userText=users[0]?.parentElement.querySelector('[data-user-message-bubble]')?.innerText.trim();
          if(userText&&userText!==current.prompt)throw Error('Ответ принадлежит другому запросу.');
          const text=assistants[0]?.parentElement.querySelector('[data-markdown-text-style="assistant-message"]')?.innerText.trim();
          const done=users.length===1&&userText===current.prompt&&assistants.length===1&&text&&
            !exactButton(['Остановить','Stop','Stop generating'])&&!!exactButton(['Копировать','Copy']);
          if(!done){stable='';since=0;return null;}
          if(text!==stable){stable=text;since=Date.now();return null;}
          return Date.now()-since>=2500 ? text:null;
        },130000);
        const accepted=await request('/result',{...lease,reply});
        if(accepted.state!=='complete')throw Error('Мост отклонил ответ: неверный JSON или идентификатор.');
        sessionStorage.removeItem(KEY+'pending');current=null;
        if(running){show('Ответ получен. Открываю новый временный чат…');location.replace(FRESH);}return;
      }
    }catch(e){
      if(current){try{await request('/fail',{worker,id:current.id,lease:current.lease});}catch{/* No resubmission. */}}
      sessionStorage.removeItem(KEY+'pending');stop(String(e.message||e));
    }
  }
  setInterval(()=>{if(running)void request('/heartbeat',{worker,status:sessionStorage.getItem(KEY+'pending')?'busy':'ready'}).catch(()=>show('Нет связи с мостом. Отправка не повторяется.'));},5000);
  if(sessionStorage.getItem(KEY+'armed')==='yes')void work();else show('Отдельная рабочая вкладка. Нажмите «Запустить», чтобы разрешить тесты.');
})();
