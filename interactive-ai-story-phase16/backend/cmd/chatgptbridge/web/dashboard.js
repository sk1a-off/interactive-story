'use strict';
const token = document.querySelector('meta[name="bridge-token"]').content;
let current = {};
const notice = document.querySelector('#notice');
async function api(path, body) {
  const r = await fetch(path, {method:body ? 'POST':'GET',headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json'},body:body ? JSON.stringify(body):undefined});
  if (!r.ok) throw Error(`${r.status}: ${await r.text()}`);
  return r.json();
}
async function refresh() {
  try {
    current = await api('/status');
    document.querySelector('#workers').textContent = `Параллельность: ${current.max_active}. Рабочие: ` + Object.entries(current.workers).map(([id,w])=>`${id.slice(0,8)}: ${w.status}${Date.now()-Date.parse(w.seen)>15000?' (нет связи)':''}`).join(' · ');
    const busy = current.jobs.some(j=>['queued','leased','dispatched'].includes(j.state));
    document.querySelector('#one').disabled=busy; document.querySelector('#three').disabled=busy;
    document.querySelector('#jobs').replaceChildren(...[...current.jobs].reverse().map(j=>{
      const card=document.createElement('article');card.className=j.state;
      const title=document.createElement('strong');title.textContent=`${j.state} · ${j.id.slice(0,10)}`;
      const details=document.createElement('p');
      details.textContent=`В очереди: ${new Date(j.created).toLocaleTimeString()}${j.finished ? ` · Всего: ${((Date.parse(j.finished)-Date.parse(j.created))/1000).toFixed(1)} с` : ''}${j.sent && j.finished ? ` · После разрешения отправки: ${((Date.parse(j.finished)-Date.parse(j.sent))/1000).toFixed(1)} с`:''}`;
      const reply=document.createElement('pre');reply.textContent=j.reply||j.error||'';card.append(title,details,reply);return card;
    }));
  } catch(e) { notice.textContent=String(e); }
}
for(const [id,count] of [['one',1],['three',3]])document.querySelector(`#${id}`).onclick=async()=>{
  try{await api('/probes',{count});notice.textContent='Тест поставлен в очередь. Оставьте рабочие вкладки открытыми.';await refresh();}catch(e){notice.textContent=String(e);}
};
document.querySelector('#copy').onclick=async()=>{try{await navigator.clipboard.writeText(token);notice.textContent='Токен скопирован. Это токен локального моста, он не даёт доступ к аккаунту ChatGPT.';}catch{notice.textContent='Буфер обмена недоступен. Локальный токен: '+token;}};
document.querySelector('#report').onclick=()=>{
  const a=document.createElement('a');const url=URL.createObjectURL(new Blob([JSON.stringify({exported:new Date().toISOString(),...current},null,2)],{type:'application/json'}));a.href=url;a.download='chatgpt-bridge-probes.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);
};
void refresh();setInterval(refresh,2000);
