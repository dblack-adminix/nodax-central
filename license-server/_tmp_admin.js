
const $=id=>document.getElementById(id);const msg=$('msg');const loginMsg=$('loginMsg');
let allItems=[],lastItems=[],curPage=0,pageSize=20,auditItems=[],auditPage=0;
const selectedIds=new Set();

function showMsg(t,e){if(msg){msg.textContent=t||'';msg.style.color=e?'#b91c1c':'#0f766e';}}
function showLoginMsg(t,e){if(loginMsg){loginMsg.textContent=t||'';loginMsg.style.color=e?'#b91c1c':'#0f766e';}}
function esc(v){return(v==null?'':String(v)).replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;');}

let _confirmResolve=null;
function askConfirm(title,text,type){return new Promise(resolve=>{
  _confirmResolve=resolve;
  const icons={danger:'❌',warn:'⚠️',info:'ℹ️'};
  $('confirmIcon').textContent=icons[type]||icons.warn;
  $('confirmIcon').className='confirm-icon '+(type||'warn');
  $('confirmTitle').textContent=title||'Подтверждение';
  $('confirmText').textContent=text||'Вы уверены?';
  const yb=$('confirmYes');
  yb.className=type==='danger'?'btn-danger':'btn';
  yb.textContent=type==='danger'?'Удалить':'Подтвердить';
  $('confirmModal').classList.add('show');
});}
function closeConfirm(val){$('confirmModal').classList.remove('show');if(_confirmResolve){_confirmResolve(val);_confirmResolve=null;}}
function switchView(a){$('loginView').style.display=a?'none':'block';$('mainView').style.display=a?'block':'none';}

async function checkAuth(){try{const r=await fetch('/api/v1/auth/me');const d=await r.json().catch(()=>({}));return!!d.authenticated;}catch(_){return false;}}

async function doLogin(){
  const u=($('loginUser')?.value||'').trim(),p=$('loginPass')?.value||'';
  if(!u||!p){showLoginMsg('Заполните логин и пароль',true);return;}
  try{const r=await fetch('/api/v1/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:u,password:p})});
  const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'Ошибка');showLoginMsg('');switchView(true);loadAll();}catch(e){showLoginMsg(e.message,true);}
}
async function doLogout(){await fetch('/api/v1/auth/logout',{method:'POST'}).catch(()=>{});switchView(false);}

async function doChangePassword(){
  const o=$('oldPass')?.value||'',n=$('newPass')?.value||'',n2=$('newPass2')?.value||'';
  if(!o||!n){showMsg('Заполните поля',true);return;}if(n!==n2){showMsg('Пароли не совпадают',true);return;}
  if(n.length<4){showMsg('Мин. 4 символа',true);return;}
  try{const r=await fetch('/api/v1/auth/change-password',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({oldPassword:o,newPassword:n})});
  const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'Ошибка');showMsg('Пароль изменен',false);$('oldPass').value='';$('newPass').value='';$('newPass2').value='';}catch(e){showMsg(e.message,true);}
}

function planLim(p){p=String(p||'').toLowerCase();return p==='pro'?30:p==='enterprise'?0:10;}
function applyPlanDef(){const pi=$('plan'),ma=$('maxAgents');if(pi&&ma)ma.value=String(planLim(pi.value));
  const tr=$('isTrial');if(tr&&tr.value==='1'){$('validDays').value='14';}}
function fmtExp(v){if(!v)return'';const t=Date.parse(v);if(!Number.isFinite(t))return esc(v);const d=Math.ceil((t-Date.now())/864e5);
  if(d>0)return esc(v.slice(0,10))+' <span class="muted">('+d+'д)</span>';if(d===0)return esc(v.slice(0,10))+' <span class="muted">(сегодня)</span>';
  return esc(v.slice(0,10))+' <span style="color:#dc2626">('+Math.abs(d)+'д назад)</span>';}

function getFiltered(){
  const q=($('searchInput')?.value||'').toLowerCase(),st=$('filterStatus')?.value||'',pl=$('filterPlan')?.value||'';
  return allItems.filter(x=>{
    if(st&&String(x.status||'').toLowerCase()!==st)return false;
    if(pl&&String(x.plan||'').toLowerCase()!==pl)return false;
    if(q&&!(x.customerName||'').toLowerCase().includes(q)&&!(x.licenseKey||'').toLowerCase().includes(q)&&!(x.notes||'').toLowerCase().includes(q))return false;
    return true;
  });
}

function renderLicenses(){
  const filtered=getFiltered();lastItems=filtered;const total=filtered.length;const pages=Math.max(1,Math.ceil(total/pageSize));
  if(curPage>=pages)curPage=pages-1;if(curPage<0)curPage=0;
  const start=curPage*pageSize,slice=filtered.slice(start,start+pageSize);
  const body=$('licensesBody');
  body.innerHTML=slice.map(x=>{
    const sc='s-'+((x.status||'unknown').toLowerCase());const rev=String(x.status||'').toLowerCase()==='revoked';
    const chk=selectedIds.has(x.id)?'checked':'';
    const host=x.lastHostname?'<span class="tag">'+esc(x.lastHostname)+'</span>':'<span class="muted">-</span>';
    const ab=rev?'<button type="button" class="btn btn-xs" data-action="restore" data-id="'+esc(x.id)+'">Вернуть</button>'
      :'<button type="button" class="btn-danger btn-xs" data-action="revoke" data-id="'+esc(x.id)+'">Отозвать</button>';
    const trial=x.isTrial?' <span class="tag">trial</span>':'';
    const cname=esc(x.customerName)+(x.customerCompany?' <span class="muted">('+esc(x.customerCompany)+')</span>':'');
    let contacts=[];
    if(x.customerEmail)contacts.push('<span class="muted">'+esc(x.customerEmail)+'</span>');
    if(x.customerTelegram)contacts.push('<span class="muted">'+esc(x.customerTelegram)+'</span>');
    if(x.customerPhone)contacts.push('<span class="muted">'+esc(x.customerPhone)+'</span>');
    const contactsHtml=contacts.length?contacts.join('<br>'):'<span class="muted">-</span>';
    return '<tr><td><input type="checkbox" class="lic-chk" data-id="'+esc(x.id)+'" '+chk+'/></td><td>'+cname+trial+'</td><td>'+contactsHtml+'</td><td><code>'+esc(x.licenseKey)+'</code></td><td>'+esc(x.plan)+'</td><td><span class="status '+sc+'">'+esc(x.status)+'</span></td><td>'+fmtExp(x.expiresAt)+'</td><td>'+host+'</td><td class="row"><button type="button" class="btn-ghost btn-xs" data-action="edit" data-id="'+esc(x.id)+'">Ред.</button><button type="button" class="btn-ghost btn-xs" data-action="extend" data-id="'+esc(x.id)+'">+30д</button>'+ab+'<button type="button" class="btn-danger btn-xs" data-action="delete" data-id="'+esc(x.id)+'">Удалить</button></td></tr>';
  }).join('');
  $('pgInfo').textContent='Стр. '+(curPage+1)+'/'+pages+' ('+total+')';
  updateBulkBar();
  recomputeFinance(allItems);
}

function updateBulkBar(){
  const cnt=selectedIds.size;$('bulkBar').style.display=cnt>0?'flex':'none';$('bulkCount').textContent=String(cnt);
}

async function loadLicenses(){
  try{const r=await fetch('/api/v1/licenses');if(r.status===401){switchView(false);return;}
  const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'HTTP '+r.status);
  allItems=(d.items||[]).slice().sort((a,b)=>(Date.parse(b?.createdAt||'')||0)-(Date.parse(a?.createdAt||'')||0));
  selectedIds.clear();renderLicenses();showMsg('Лицензий: '+allItems.length,false);}catch(e){showMsg(e.message,true);}
}

async function createLicense(){
  try{applyPlanDef();const rawD=parseInt(($('validDays')?.value||'').trim(),10);
  const vd=Number.isFinite(rawD)&&rawD>0?rawD:365;const ea=new Date(Date.now()+vd*864e5).toISOString();
  const trial=$('isTrial')?.value==='1';
  const pl={customerName:$('customer').value.trim(),customerEmail:$('custEmail').value.trim(),customerTelegram:$('custTg').value.trim(),customerPhone:$('custPhone').value.trim(),customerCompany:$('custCompany').value.trim(),plan:$('plan').value,maxAgents:Number($('maxAgents').value||0),validDays:trial?14:vd,expiresAt:trial?new Date(Date.now()+14*864e5).toISOString():ea,notes:$('notes').value.trim()};
  if(!pl.customerName)throw new Error('Укажите клиента');
  const r=await fetch('/api/v1/licenses',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(pl)});
  const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'HTTP '+r.status);
  showMsg('Создана: '+(d.licenseKey||''),false);await loadLicenses();}catch(e){showMsg(e.message,true);}
}

async function licAction(id,action){
  if(action==='revoke'){if(!await askConfirm('Отозвать лицензию','Лицензия будет деактивирована. Можно вернуть позже.','warn'))return;}
  if(action==='restore'){if(!await askConfirm('Вернуть лицензию','Лицензия снова станет активной.','info'))return;}
  if(action==='delete'){if(!await askConfirm('Удалить лицензию','Лицензия будет удалена безвозвратно. Это действие нельзя отменить.','danger'))return;
    try{const r=await fetch('/api/v1/licenses/'+encodeURIComponent(id),{method:'DELETE'});const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'Err');showMsg('Удалена',false);await loadLicenses();}catch(e){showMsg(e.message,true);}return;}
  if(action==='edit'){openEditModal(id);return;}
  try{const opts={method:'POST',headers:{'Content-Type':'application/json'}};
  if(action==='extend')opts.body=JSON.stringify({days:30});
  const r=await fetch('/api/v1/licenses/'+encodeURIComponent(id)+'/'+action,opts);
  const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'HTTP '+r.status);
  showMsg(action==='extend'?'Продлена':action==='revoke'?'Отозвана':'Возвращена',false);await loadLicenses();}catch(e){showMsg(e.message,true);}
}

async function bulkAction(action){
  if(!selectedIds.size)return;
  if(action==='revoke'){if(!await askConfirm('Массовый отзыв','Отозвать '+selectedIds.size+' лицензий?','warn'))return;}
  if(action==='extend'){if(!await askConfirm('Массовое продление','Продлить '+selectedIds.size+' лицензий на 30 дней?','info'))return;}
  for(const id of selectedIds){
    try{const opts={method:'POST',headers:{'Content-Type':'application/json'}};
    if(action==='extend')opts.body=JSON.stringify({days:30});
    await fetch('/api/v1/licenses/'+encodeURIComponent(id)+'/'+action,opts);}catch(_){}
  }
  selectedIds.clear();await loadLicenses();showMsg('Массовое действие выполнено',false);
}

function openEditModal(id){
  const lic=allItems.find(x=>x.id===id);if(!lic)return;
  $('edId').value=id;$('edCustomer').value=lic.customerName||'';$('edCompany').value=lic.customerCompany||'';
  $('edEmail').value=lic.customerEmail||'';$('edTg').value=lic.customerTelegram||'';$('edPhone').value=lic.customerPhone||'';
  $('edPlan').value=lic.plan||'basic';$('edMaxAgents').value=String(lic.maxAgents||0);$('edNotes').value=lic.notes||'';
  $('editModal').classList.add('show');
}
async function saveEdit(){
  const id=$('edId').value;if(!id)return;
  try{const r=await fetch('/api/v1/licenses/'+encodeURIComponent(id),{method:'PATCH',headers:{'Content-Type':'application/json'},
    body:JSON.stringify({customerName:$('edCustomer').value.trim(),customerCompany:$('edCompany').value.trim(),customerEmail:$('edEmail').value.trim(),customerTelegram:$('edTg').value.trim(),customerPhone:$('edPhone').value.trim(),plan:$('edPlan').value,maxAgents:Number($('edMaxAgents').value||0),notes:$('edNotes').value.trim()})});
  const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'Ошибка');
  $('editModal').classList.remove('show');showMsg('Обновлено',false);await loadLicenses();}catch(e){showMsg(e.message,true);}
}

function finCfg(){const b=Number($('priceBasic')?.value||0),p=Number($('pricePro')?.value||0),e=Number($('priceEnterprise')?.value||0);
  const c=String($('priceCurrency')?.value||'RUB').trim().toUpperCase()||'RUB';
  return{basic:b>=0?b:0,pro:p>=0?p:0,enterprise:e>=0?e:0,currency:c};}
function saveFin(){localStorage.setItem('license_finance_cfg',JSON.stringify(finCfg()));recomputeFinance(allItems);showMsg('Цены сохранены',false);}
function loadFin(){const raw=localStorage.getItem('license_finance_cfg');if(!raw){if($('priceBasic'))$('priceBasic').value='1000';if($('pricePro'))$('pricePro').value='3000';if($('priceEnterprise'))$('priceEnterprise').value='5000';if($('priceCurrency'))$('priceCurrency').value='RUB';return;}try{const c=JSON.parse(raw);
  if($('priceBasic'))$('priceBasic').value=String(c.basic??0);if($('pricePro'))$('pricePro').value=String(c.pro??0);
  if($('priceEnterprise'))$('priceEnterprise').value=String(c.enterprise??0);
  const cur=String(c.currency||'').trim().toUpperCase();const m=!cur||cur==='USD'||cur==='$'?'RUB':cur;
  if($('priceCurrency'))$('priceCurrency').value=m;if(m!==cur){c.currency=m;localStorage.setItem('license_finance_cfg',JSON.stringify(c));}}catch(_){}}
function money(v,c){return new Intl.NumberFormat('ru-RU',{style:'currency',currency:c,maximumFractionDigits:0}).format(v);}
function recomputeFinance(items){
  const cfg=finCfg();const active=(items||[]).filter(x=>String(x?.status||'').toLowerCase()==='active');
  let arr=0;for(const x of active){const p=String(x?.plan||'').toLowerCase();arr+=p==='pro'?cfg.pro:p==='enterprise'?cfg.enterprise:cfg.basic;}
  const cnt=active.length,mrr=arr/12,arpl=cnt>0?(arr/cnt):0;
  if($('kpiActive'))$('kpiActive').textContent=String(cnt);if($('kpiMRR'))$('kpiMRR').textContent=money(mrr,cfg.currency);
  if($('kpiARR'))$('kpiARR').textContent=money(arr,cfg.currency);if($('kpiARPL'))$('kpiARPL').textContent=money(arpl,cfg.currency);
  drawCharts(items);
}

function drawCharts(items){
  const plans={basic:0,pro:0,enterprise:0};const statuses={active:0,revoked:0,expired:0};
  for(const x of(items||[])){const p=String(x.plan||'basic').toLowerCase();plans[p]=(plans[p]||0)+1;
    let st=String(x.status||'').toLowerCase();if(st==='active'){const exp=Date.parse(x.expiresAt||'');if(exp&&exp<Date.now())st='expired';}
    statuses[st]=(statuses[st]||0)+1;}
  drawDonut($('chartDonut'),plans,{basic:'#0891b2',pro:'#0f766e',enterprise:'#6366f1'});
  drawDonut($('chartStatus'),statuses,{active:'#16a34a',revoked:'#dc2626',expired:'#d97706'});
}
function drawDonut(canvas,data,colors){
  if(!canvas)return;const ctx=canvas.getContext('2d');const w=canvas.width,h=canvas.height;ctx.clearRect(0,0,w,h);
  const cx=80,cy=h/2,r=55,ir=32;const entries=Object.entries(data).filter(e=>e[1]>0);const total=entries.reduce((s,e)=>s+e[1],0);
  if(!total){ctx.fillStyle='#94a3b8';ctx.font='12px sans-serif';ctx.fillText('Нет данных',cx-25,cy+4);return;}
  let angle=-Math.PI/2;for(const[k,v]of entries){const slice=v/total*Math.PI*2;
    ctx.beginPath();ctx.moveTo(cx,cy);ctx.arc(cx,cy,r,angle,angle+slice);ctx.closePath();ctx.fillStyle=colors[k]||'#94a3b8';ctx.fill();angle+=slice;}
  ctx.beginPath();ctx.arc(cx,cy,ir,0,Math.PI*2);ctx.fillStyle='#f8fbff';ctx.fill();
  ctx.fillStyle='#0f172a';ctx.font='bold 16px sans-serif';ctx.textAlign='center';ctx.fillText(String(total),cx,cy+6);ctx.textAlign='start';
  let ly=20;for(const[k,v]of entries){ctx.fillStyle=colors[k]||'#94a3b8';ctx.fillRect(160,ly-8,10,10);
    ctx.fillStyle='#334155';ctx.font='12px sans-serif';ctx.fillText(k+': '+v+' ('+Math.round(v/total*100)+'%)',175,ly);ly+=20;}
}

async function loadAudit(){
  try{const r=await fetch('/api/v1/audit');const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'Err');
  auditItems=d.items||[];renderAudit();}catch(e){showMsg(e.message,true);}
}
function renderAudit(){
  const total=auditItems.length;const pages=Math.max(1,Math.ceil(total/pageSize));
  if(auditPage>=pages)auditPage=pages-1;if(auditPage<0)auditPage=0;
  const s=auditPage*pageSize,slice=auditItems.slice(s,s+pageSize);
  $('auditBody').innerHTML=slice.map(x=>'<tr><td>'+esc((x.createdAt||'').slice(0,19).replace('T',' '))+'</td><td><b>'+esc(x.action)+'</b></td><td><code>'+esc((x.licenseId||'').slice(0,8))+'</code></td><td>'+esc(x.actor)+'</td><td class="muted">'+esc(x.details)+'</td></tr>').join('');
  $('auditInfo').textContent='Стр. '+(auditPage+1)+'/'+pages+' ('+total+')';
}

async function loadSettings(){
  try{const r=await fetch('/api/v1/settings');const d=await r.json().catch(()=>({}));
  if($('tgToken'))$('tgToken').value=d.telegram_bot_token||'';if($('tgChat'))$('tgChat').value=d.telegram_chat_id||'';
  if($('tgDays'))$('tgDays').value=d.notify_days_before||'7';if($('whUrl'))$('whUrl').value=d.webhook_url||'';}catch(_){}
}
async function saveSettings(obj){
  try{const r=await fetch('/api/v1/settings',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(obj)});
  const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'Err');showMsg('Сохранено',false);}catch(e){showMsg(e.message,true);}
}
async function loadAPIKeys(){
  try{const r=await fetch('/api/v1/api-keys');const d=await r.json().catch(()=>({}));
  const keys=d.items||[];$('apiKeysList').innerHTML=keys.length?keys.map(k=>
    '<div class="row" style="margin-bottom:6px;font-size:12px"><b>'+esc(k.name)+'</b> <code>'+esc(k.key)+'</code> <span class="tag">'+esc(k.role)+'</span> <button type="button" class="btn-danger btn-xs" data-delkey="'+esc(k.id)+'">X</button></div>'
  ).join(''):'<div class="muted">Нет API-ключей</div>';}catch(_){}
}
async function createAPIKey(){
  const name=($('akName')?.value||'').trim(),role=$('akRole')?.value||'readonly';
  if(!name){showMsg('Укажите имя ключа',true);return;}
  try{const r=await fetch('/api/v1/api-keys',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name,role})});
  const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'Err');showMsg('Ключ создан: '+d.key,false);$('akName').value='';loadAPIKeys();}catch(e){showMsg(e.message,true);}
}
async function deleteAPIKey(id){
  if(!await askConfirm('Удалить API-ключ','Ключ будет удалён и перестанет работать.','danger'))return;
  try{await fetch('/api/v1/api-keys/'+encodeURIComponent(id),{method:'DELETE'});loadAPIKeys();}catch(_){}
}

function loadAll(){loadFin();loadLicenses();loadAudit();loadSettings();loadAPIKeys();}

// Event listeners
document.querySelectorAll('.tab').forEach(tab=>tab.addEventListener('click',()=>{
  document.querySelectorAll('.tab').forEach(t=>t.classList.remove('active'));
  document.querySelectorAll('.tab-content').forEach(t=>t.classList.remove('active'));
  tab.classList.add('active');const tgt=$('tab-'+tab.getAttribute('data-tab'));if(tgt)tgt.classList.add('active');
}));
document.querySelectorAll('.nav-item').forEach(btn=>btn.addEventListener('click',()=>{
  const tabName=btn.getAttribute('data-tab');
  document.querySelectorAll('.nav-item').forEach(b=>b.classList.remove('active'));
  document.querySelectorAll('.tab-content').forEach(t=>t.classList.remove('active'));
  btn.classList.add('active');
  const tgt=$('tab-'+tabName);if(tgt)tgt.classList.add('active');
}));

$('btnLogin')?.addEventListener('click',doLogin);
$('loginPass')?.addEventListener('keydown',e=>{if(e.key==='Enter')doLogin();});
$('btnLogout')?.addEventListener('click',doLogout);
$('btnCreate')?.addEventListener('click',createLicense);
$('btnRefresh')?.addEventListener('click',loadLicenses);
$('btnExportToggle')?.addEventListener('click',e=>{e.stopPropagation();$('exportMenu').classList.toggle('show');});
document.addEventListener('click',()=>$('exportMenu')?.classList.remove('show'));
$('exportMenu')?.addEventListener('click',e=>{const a=e.target.closest('[data-fmt]');if(!a)return;const fmt=a.getAttribute('data-fmt');window.open('/api/v1/licenses/export?format='+fmt,'_blank');$('exportMenu').classList.remove('show');});
$('btnSaveFinance')?.addEventListener('click',saveFin);
$('btnChangePass')?.addEventListener('click',doChangePassword);
$('btnRefreshAudit')?.addEventListener('click',loadAudit);
$('plan')?.addEventListener('change',applyPlanDef);
$('isTrial')?.addEventListener('change',applyPlanDef);
$('searchInput')?.addEventListener('input',()=>{curPage=0;renderLicenses();});
$('filterStatus')?.addEventListener('change',()=>{curPage=0;renderLicenses();});
$('filterPlan')?.addEventListener('change',()=>{curPage=0;renderLicenses();});
$('pgPrev')?.addEventListener('click',()=>{curPage--;renderLicenses();});
$('pgNext')?.addEventListener('click',()=>{curPage++;renderLicenses();});
$('auditPrev')?.addEventListener('click',()=>{auditPage--;renderAudit();});
$('auditNext')?.addEventListener('click',()=>{auditPage++;renderAudit();});
$('btnEdCancel')?.addEventListener('click',()=>$('editModal').classList.remove('show'));
$('btnEdSave')?.addEventListener('click',saveEdit);
$('editModal')?.addEventListener('click',e=>{if(e.target===$('editModal'))$('editModal').classList.remove('show');});
$('confirmYes')?.addEventListener('click',()=>closeConfirm(true));
$('confirmNo')?.addEventListener('click',()=>closeConfirm(false));
$('confirmModal')?.addEventListener('click',e=>{if(e.target===$('confirmModal'))closeConfirm(false);});
$('btnCreateAK')?.addEventListener('click',createAPIKey);
$('apiKeysList')?.addEventListener('click',e=>{const btn=e.target.closest('[data-delkey]');if(btn)deleteAPIKey(btn.getAttribute('data-delkey'));});
$('btnSaveTg')?.addEventListener('click',()=>saveSettings({telegram_bot_token:$('tgToken').value.trim(),telegram_chat_id:$('tgChat').value.trim(),notify_days_before:$('tgDays').value.trim()}));
$('btnTestTg')?.addEventListener('click',async()=>{try{const r=await fetch('/api/v1/test-telegram',{method:'POST'});const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'Err');showMsg('Telegram OK',false);}catch(e){showMsg(e.message,true);}});
$('btnBackup')?.addEventListener('click',()=>{window.open('/api/v1/backup','_blank');});
$('btnRestoreBtn')?.addEventListener('click',()=>$('restoreFile').click());
$('restoreFile')?.addEventListener('change',async e=>{
  const f=e.target.files[0];if(!f)return;if(!await askConfirm('Восстановить БД','Все текущие данные будут перезаписаны из файла бэкапа. Продолжить?','danger'))return;
  try{const body=await f.text();const r=await fetch('/api/v1/restore',{method:'POST',headers:{'Content-Type':'application/json'},body});
  const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'Err');showMsg('БД восстановлена',false);loadAll();}catch(e2){showMsg(e2.message,true);}
  $('restoreFile').value='';
});

$('licensesBody')?.addEventListener('click',evt=>{
  const btn=evt.target.closest('button[data-action]');if(btn){const id=btn.getAttribute('data-id'),act=btn.getAttribute('data-action');if(id&&act)licAction(id,act);return;}
});
$('licensesBody')?.addEventListener('change',evt=>{
  const chk=evt.target.closest('.lic-chk');if(!chk)return;const id=chk.getAttribute('data-id');
  if(chk.checked)selectedIds.add(id);else selectedIds.delete(id);updateBulkBar();
});
$('thCheckAll')?.addEventListener('change',e=>{
  const chks=document.querySelectorAll('.lic-chk');chks.forEach(c=>{c.checked=e.target.checked;
    const id=c.getAttribute('data-id');if(e.target.checked)selectedIds.add(id);else selectedIds.delete(id);});updateBulkBar();
});
$('bulkBar')?.addEventListener('click',e=>{const btn=e.target.closest('[data-bulk]');if(btn)bulkAction(btn.getAttribute('data-bulk'));});

applyPlanDef();
(async()=>{const a=await checkAuth();switchView(a);if(a)loadAll();})();
