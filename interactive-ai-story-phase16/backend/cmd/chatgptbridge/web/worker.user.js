// ==UserScript==
// @name         Interactive Story ChatGPT Probe Worker
// @namespace    local.interactive-story
// @version      0.1.4
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
  const title=document.createElement('strong');title.textContent='Мост 0.1.4 · '+worker.slice(0,8)+' · только тесты';
  const status=document.createElement('p');
  const button=document.createElement('button');button.type='button';button.textContent='Запустить';
  Object.assign(button.style,{display:'inline-block',padding:'7px 12px',marginTop:'8px',background:'#315587',color:'#fff',border:'1px solid #8eb7ed',borderRadius:'7px',cursor:'pointer',pointerEvents:'auto'});
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
  function instantLabel(){
    const picker=exactButton(['Выбрать модель ChatGPT','Choose ChatGPT model']);
    return picker&&/^Instant$/.test(picker.innerText.trim());
  }
  async function selectGPT6Instant(){
    const getPicker=()=>exactButton(['Выбрать модель ChatGPT','Choose ChatGPT model']);
    const visible=selector=>[...document.querySelectorAll(selector)].find(e=>!e.closest('[inert],[aria-hidden="true"]'));
    const getToggle=()=>visible('[data-model-picker-view-toggle]');
    const getModel=()=>[...document.querySelectorAll('[role="menuitemradio"]')].find(e=>!e.closest('[inert],[aria-hidden="true"]')&&e.innerText.trim()==='GPT-6'&&e.getAttribute('aria-disabled')!=='true');
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
      if(picker.getAttribute('aria-expanded')!=='true')picker.click();
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
      stage='установка мощности Instant';
      const power=await waitFor(()=>visible('[data-reasoning-slider="true"]'),5000);
      // The real slider input itself is aria-hidden; its active parent is not.
      const slider=()=>[...document.querySelectorAll('[data-model-picker-power-slider] [role="slider"]')].find(e=>!e.closest('[inert]')&&!e.parentElement.closest('[aria-hidden="true"]'));
      const minimum=()=>{const e=slider();return e&&e.getAttribute('aria-valuenow')===(e.getAttribute('aria-valuemin')||'0');};
      power.focus();
      for(let i=0;i<3&&!minimum();i++){
        power.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowLeft',code:'ArrowLeft',bubbles:true}));
        power.dispatchEvent(new KeyboardEvent('keyup',{key:'ArrowLeft',code:'ArrowLeft',bubbles:true}));
        await pause(150);
      }
      // Live announcements are optional and may not contain a mode index.
      // Confirm the rendered mode and minimum slider value instead.
      await waitFor(()=>minimum()&&getToggle()?.innerText.trim()==='Instant',5000);
      stage='закрытие меню модели';
      await closeMenu();
      await waitFor(instantLabel,3000);
    }catch(e){
      try{await closeMenu();}catch{/* Preserve the original failed stage; never send. */}
      throw Error('GPT-6 Instant: '+stage+'. '+(e.message||e));
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
      while(running){
        if(!empty())throw Error('Нужен пустой подтверждённый временный чат.');
        current=await request('/claim',{worker});
        if(!current){show('Готов. Ожидание синтетического задания…');await pause(1500);continue;}
        const lease={worker,id:current.id,lease:current.lease};
        sessionStorage.setItem(KEY+'pending',JSON.stringify(lease));
        show('Выбор GPT-6 Instant · '+current.id.slice(0,8));
        await selectGPT6Instant();
        if(!running||!empty())throw Error('Страница изменилась до отправки.');
        const box=editor();box.focus();
        // Use the browser editing command so the React composer receives a native input event.
        if(!document.execCommand('insertText',false,current.prompt))throw Error('Не удалось заполнить поле.');
        if(box.textContent.trim()!==current.prompt)throw Error('Текст поля не совпадает с заданием.');
        const send=await waitFor(()=>{const e=exactButton(['Отправить','Send']);return e&&!e.disabled&&e;},5000);
        if(!running||!temporary()||!instantLabel()||roles('user').length||roles('assistant').length)throw Error('Проверка перед отправкой не пройдена.');
        show('GPT-6 Instant · '+current.id.slice(0,8)+' · ожидание ответа…');
        await request('/send',lease); // A failed/ambiguous authorization NEVER leads to a click or retry.
        if(!running||!temporary()||!instantLabel()||box.textContent.trim()!==current.prompt)throw Error('Отправка остановлена после разрешения.');
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
