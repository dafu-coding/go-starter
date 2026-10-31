'use strict';
const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];
const storage = {
  get(key, fallback = '') { try { return localStorage.getItem(`launchboard_${key}`) || fallback; } catch { return fallback; } },
  set(key, value) { try { localStorage.setItem(`launchboard_${key}`, value); } catch { /* Browser storage is optional. */ } },
  remove(key) { try { localStorage.removeItem(`launchboard_${key}`); } catch { /* Browser storage is optional. */ } }
};
const state = { tasks: [], view: 'all', category: 'all', assignee: 'all', priority: 'all', search: '', sort: 'priority', layout: storage.get('layout', 'board'), name: storage.get('name'), loaded: false, protected: false };
const statusNames = { idea: '未着手', building: '進行中', shipped: '完了' };
const priorityNames = { high: '高', normal: '通常', low: '低' };
const viewNames = { all: 'すべてのタスク', mine: '自分の担当', due: '今日までの期限', shipped: '完了したタスク', archived: 'アーカイブ' };
const busy = new Set();
let editing = null;
let requestNumber = 0;
let toastTimer;
let toastAction;
function el(tag, className, text) { const node = document.createElement(tag); if (className) node.className = className; if (text !== undefined) node.textContent = text; return node; }
function today() { const date = new Date(); return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`; }
function shortDate(value) { if (!value) return '期限なし'; const [year, month, day] = value.split('-').map(Number); return new Date(year, month - 1, day).toLocaleDateString('ja-JP', { month: 'numeric', day: 'numeric' }); }
function fullDate(value) { return new Date(value).toLocaleString('ja-JP', { year: 'numeric', month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' }); }
function hasDialog() { return $$('dialog').some(dialog => dialog.open); }
function notify(message, action) {
  clearTimeout(toastTimer); $('#toast > span').textContent = message; $('#toast').hidden = false;
  toastAction = action; $('#toast-action').hidden = !action;
  toastTimer = setTimeout(() => { $('#toast').hidden = true; toastAction = null; }, action ? 10000 : 4500);
}
$('#toast-action').addEventListener('click', async () => {
  if (!toastAction) return; const action = toastAction; toastAction = null; $('#toast').hidden = true;
  try { await action(); } catch (error) { notify(error.message); }
});
async function api(path, options = {}) {
  const response = await fetch(path, { ...options, headers: { 'Content-Type': 'application/json', ...options.headers } });
  let data; try { data = await response.json(); } catch { throw new Error('サーバーから応答を受け取れませんでした。'); }
  if (!response.ok) {
    if (response.status === 401 && path !== '/api/session') showLogin();
    const error = new Error(data.error || '通信に失敗しました。もう一度お試しください。'); error.status = response.status; throw error;
  }
  return data;
}
function showLogin() {
  state.tasks = []; state.loaded = false;
  $$('dialog').forEach(dialog => { if (dialog.open) dialog.close(); });
  $('#workspace').hidden = true; $('#startup').hidden = true; $('#login-screen').hidden = false;
  $('#login-form input').focus();
}
async function boot() {
  try {
    const session = await api('/api/session'); state.protected = session.protected;
    $('#access-label').textContent = session.protected ? 'パスワード保護中' : '共有ワークスペース'; $('#logout').hidden = !session.protected;
    if (!session.authenticated) { showLogin(); return; }
    $('#startup').hidden = true; $('#login-screen').hidden = true; $('#workspace').hidden = false;
    updateProfile(); await load();
  } catch (error) { $('#startup').textContent = `${error.message} ページを再読み込みしてください。`; }
}
$('#login-form').addEventListener('submit', async event => {
  event.preventDefault(); const button = $('#login-form button[type=submit]'); button.disabled = true; $('#login-error').textContent = '';
  try { await api('/api/session', { method: 'POST', body: JSON.stringify({ password: $('#login-form input').value }) }); $('#login-form').reset(); await boot(); }
  catch (error) { $('#login-error').textContent = error.message; }
  finally { button.disabled = false; }
});
$('#logout').addEventListener('click', async () => { try { await api('/api/session', { method: 'DELETE' }); showLogin(); } catch (error) { notify(error.message); } });
async function load({ silent = false } = {}) {
  const number = ++requestNumber; $('#refresh').disabled = true;
  try {
    const tasks = await api('/api/ideas'); if (number !== requestNumber) return;
    state.tasks = tasks; state.loaded = true; $('#load-error').hidden = true;
    syncAssignees(); render(); $('#last-updated').textContent = `最終更新 ${new Date().toLocaleTimeString('ja-JP', { hour: '2-digit', minute: '2-digit' })}`;
  } catch (error) {
    if (number !== requestNumber || error.status === 401) return;
    $('#load-error').textContent = `${error.message} ↻ から再試行できます。`; $('#load-error').hidden = false;
    if (!silent) notify(error.message);
  } finally { if (number === requestNumber) { $('#refresh').disabled = false; $('#board').setAttribute('aria-busy', 'false'); } }
}
function syncAssignees() {
  const names = [...new Set(state.tasks.map(task => task.assignee).filter(Boolean))].sort((a, b) => a.localeCompare(b, 'ja'));
  $('#assignees').replaceChildren(...names.map(name => { const option = el('option'); option.value = name; return option; }));
  const filter = $('#assignee-filter'); filter.replaceChildren(new Option('すべての担当者', 'all'), new Option('未割り当て', ''), ...names.map(name => new Option(name, name)));
  if (state.assignee !== 'all' && state.assignee !== '' && !names.includes(state.assignee)) filter.append(new Option(state.assignee, state.assignee));
  filter.value = state.assignee;
}
function filteredTasks() {
  return state.tasks.filter(task =>
    (state.view === 'archived' ? task.archived : !task.archived) &&
    (state.view !== 'mine' || task.assignee === state.name) &&
    (state.view !== 'due' || (task.dueDate && task.dueDate <= today() && task.status !== 'shipped')) &&
    (state.view !== 'shipped' || task.status === 'shipped') &&
    (state.category === 'all' || task.category === state.category) &&
    (state.assignee === 'all' || task.assignee === state.assignee) &&
    (state.priority === 'all' || task.priority === state.priority) &&
    (!state.search || `${task.title} ${task.description} ${task.assignee}`.toLocaleLowerCase().includes(state.search))
  ).sort((a, b) => {
    if (state.sort === 'recent') return b.id - a.id;
    const priority = { high: 0, normal: 1, low: 2 };
    if (state.sort === 'priority' && priority[a.priority] !== priority[b.priority]) return priority[a.priority] - priority[b.priority];
    return (a.dueDate || '9999-12-31').localeCompare(b.dueDate || '9999-12-31') || b.id - a.id;
  });
}
function render() {
  const active = state.tasks.filter(task => !task.archived);
  const open = active.filter(task => task.status !== 'shipped');
  const done = active.length - open.length;
  $('#stat-open').textContent = open.length; $('#nav-total').textContent = open.length;
  $('#stat-done').textContent = done;
  $('#stat-overdue').textContent = open.filter(task => task.dueDate && task.dueDate < today()).length;
  $('#nav-due').textContent = open.filter(task => task.dueDate && task.dueDate <= today()).length;
  $('#today-count').textContent = `今日の期限 ${open.filter(task => task.dueDate === today()).length}件`;
  $('#completion-rate').textContent = `完了率 ${active.length ? Math.round(done / active.length * 100) : 0}%`;
  $('#today-label').textContent = new Date().toLocaleDateString('ja-JP', { month: 'long', day: 'numeric', weekday: 'long' }).toUpperCase();
  $('#sample-banner').hidden = !active.some(task => task.sample) || state.view === 'archived';
  $$('[data-view]').forEach(button => button.classList.toggle('active', button.dataset.view === state.view));
  $('#page-name').textContent = viewNames[state.view]; $('#board-title').textContent = viewNames[state.view];
  const filtered = filteredTasks(); $('#result-count').textContent = `${filtered.length}件`;
  const constrained = state.search || state.category !== 'all' || state.assignee !== 'all' || state.priority !== 'all';
  $('#clear-filters').hidden = !constrained;
  const empty = state.loaded && !filtered.length;
  $('#empty-state').hidden = !empty;
  const brandNew = !active.length && state.view === 'all' && !constrained;
  $('#empty-title').textContent = brandNew ? '最初のタスクから、はじめよう。' : state.view === 'archived' ? 'アーカイブは空です。' : state.view === 'due' ? '今日までの期限は、すべてクリア。' : '表示するタスクがありません。';
  $('#empty-description').textContent = brandNew ? 'やることを追加して、担当と期限を決めましょう。' : constrained ? '検索や絞り込みの条件を変えてみてください。' : state.view === 'mine' ? `担当者を「${state.name}」にすると、ここに表示されます。` : state.view === 'archived' ? '保管したタスクはここからいつでも復元できます。' : '新しいタスクを追加するか、ほかの表示を確認しましょう。';
  $('#load-samples').hidden = !brandNew || state.tasks.some(task => task.sample);
  const list = state.layout === 'list' || state.view === 'archived';
  $('#board').hidden = empty || list; $('#task-list').hidden = empty || !list;
  $('#view-board').classList.toggle('selected', !list); $('#view-list').classList.toggle('selected', list);
  $('#view-board').setAttribute('aria-pressed', String(!list)); $('#view-list').setAttribute('aria-pressed', String(list));
  for (const column of $$('[data-column]')) {
    const tasks = filtered.filter(task => task.status === column.dataset.column);
    $('.column-count', column).textContent = tasks.length;
    $('.cards', column).replaceChildren(...tasks.map(card));
    if (!tasks.length) $('.cards', column).append(el('div', 'column-empty', 'タスクはありません'));
  }
  $('#list-rows').replaceChildren(...filtered.map(listRow));
}
function setDue(node, task) {
  const overdue = task.status !== 'shipped' && task.dueDate && task.dueDate < today();
  const isToday = task.status !== 'shipped' && task.dueDate === today();
  node.textContent = `${task.dueDate ? '◷ ' : ''}${shortDate(task.dueDate)}${overdue ? ' · 期限切れ' : isToday ? ' · 今日' : ''}`;
  node.classList.toggle('overdue', Boolean(overdue)); node.classList.toggle('today', Boolean(isToday));
}
function assigneeNode(task) {
  const label = el('span', 'assignee-label'); const avatar = el('span', 'avatar', task.assignee ? [...task.assignee][0] : '–');
  label.append(avatar, el('span', '', task.assignee || '未割り当て')); return label;
}
function statusSelect(task) {
  const select = el('select', 'status-select');
  for (const [status, name] of Object.entries(statusNames)) select.append(new Option(`${{ idea: '○', building: '◴', shipped: '✓' }[status]} ${name}`, status));
  select.value = task.status; select.setAttribute('aria-label', `${task.title} のステータス`);
  select.disabled = busy.has(task.id);
  select.addEventListener('change', async () => {
    select.disabled = true; busy.add(task.id);
    try { await patch(task, { status: select.value }); notify(`「${statusNames[select.value]}」に変更しました`); }
    catch (error) { notify(error.message); select.value = task.status; }
    finally { busy.delete(task.id); select.disabled = false; }
  });
  return select;
}
async function patch(task, fields) {
  const result = await api(`/api/ideas/${task.id}`, { method: 'PATCH', body: JSON.stringify({ ...fields, version: task.version }) });
  await load({ silent: true }); return result;
}
function card(task) {
  const fragment = $('#card-template').content.cloneNode(true); const article = $('article', fragment); article.dataset.id = task.id;
  $('.category-label', fragment).textContent = task.category; $('.category-label', fragment).classList.add(task.category);
  const priority = $('.priority-label', fragment); priority.textContent = `${task.priority === 'high' ? '↑' : task.priority === 'low' ? '↓' : '−'} ${priorityNames[task.priority]}`; priority.classList.add(task.priority);
  const title = $('.task-title', fragment); title.textContent = task.title; title.addEventListener('click', () => openTask(task));
  $('.card-description', fragment).textContent = task.description;
  setDue($('.due-label', fragment), task); $('.assignee-label', fragment).replaceWith(assigneeNode(task));
  $('.card-id', fragment).textContent = `LB-${String(task.id).padStart(3, '0')}`;
  $('.status-select', fragment).replaceWith(statusSelect(task)); return fragment;
}
function listRow(task) {
  const row = el('div', 'list-row'); const title = el('button', 'list-title', task.title);
  title.append(el('small', '', `LB-${String(task.id).padStart(3, '0')} · ${task.category}`)); title.addEventListener('click', () => openTask(task));
  const due = el('span', 'due-label'); setDue(due, task);
  const priority = el('span', `priority-label ${task.priority}`, priorityNames[task.priority]);
  let status;
  if (task.archived) { status = el('button', 'restore-task', '復元する ↗'); status.addEventListener('click', async () => { status.disabled = true; try { await patch(task, { archived: false }); notify('タスクを復元しました'); } catch (error) { notify(error.message); } finally { status.disabled = false; } }); }
  else status = statusSelect(task);
  row.append(title, assigneeNode(task), due, priority, status); return row;
}
const taskDialog = $('#task-dialog'); const form = $('#task-form');
function openTask(task = null) {
  editing = task; form.reset(); $('#form-error').textContent = '';
  $('#dialog-title').textContent = task ? task.archived ? '保管したタスク' : 'タスクを編集' : 'タスクを追加';
  $('#dialog-kicker').textContent = task ? `LB-${String(task.id).padStart(3, '0')} / TASK DETAILS` : 'NEW TASK';
  $('#dialog-description').textContent = task ? '内容を更新して、次の一歩を明確に。' : '小さく分けて、一歩ずつ進めよう。';
  $('#status-field').hidden = !task; $('#archive-task').hidden = !task; $('#submit-task').hidden = Boolean(task?.archived);
  $('#archive-task').textContent = task?.archived ? '復元する' : 'アーカイブ'; $('#submit-task').textContent = task ? '変更を保存' : '追加する';
  $$('input,textarea,select', form).forEach(input => { input.disabled = Boolean(task?.archived); });
  let fields = task;
  if (!task) { try { fields = JSON.parse(storage.get('draft', '{}')); } catch { fields = {}; } }
  for (const name of ['title', 'description', 'assignee', 'priority', 'category', 'dueDate', 'status']) if (fields?.[name] !== undefined && form.elements[name]) form.elements[name].value = fields[name];
  $('#task-meta').textContent = task ? `作成 ${fullDate(task.createdAt)} · 更新 ${fullDate(task.updatedAt)}` : '入力中の内容は、このブラウザに下書き保存されます。';
  taskDialog.showModal(); form.elements.title.focus();
}
function draftFields() { return Object.fromEntries(['title', 'description', 'assignee', 'priority', 'category', 'dueDate'].map(name => [name, form.elements[name].value])); }
form.addEventListener('input', () => { if (!editing) storage.set('draft', JSON.stringify(draftFields())); });
form.addEventListener('submit', async event => {
  event.preventDefault(); const button = $('#submit-task'); button.disabled = true; $('#form-error').textContent = '';
  try {
    const fields = draftFields();
    if (editing) await patch(editing, { ...fields, status: form.elements.status.value });
    else { await api('/api/ideas', { method: 'POST', body: JSON.stringify({ ...fields, author: state.name || 'Guest' }) }); storage.remove('draft'); await load(); }
    taskDialog.close(); notify(editing ? 'タスクを更新しました' : 'タスクを追加しました');
  } catch (error) { $('#form-error').textContent = error.message; if (error.status === 409) await load({ silent: true }); }
  finally { button.disabled = false; }
});
$('#archive-task').addEventListener('click', async () => {
  if (!editing) return; const task = editing; const button = $('#archive-task'); button.disabled = true;
  try {
    const archived = !task.archived; const result = await patch(task, { archived }); taskDialog.close();
    notify(archived ? 'タスクをアーカイブしました' : 'タスクを復元しました', archived ? async () => { await patch({ ...task, version: result.version }, { archived: false }); notify('アーカイブを取り消しました'); } : null);
  } catch (error) { $('#form-error').textContent = error.message; }
  finally { button.disabled = false; }
});
$$('[data-create]').forEach(button => button.addEventListener('click', () => openTask()));
$$('[data-close]').forEach(button => button.addEventListener('click', () => $(`#${button.dataset.close}`).close()));
$$('dialog').forEach(dialog => dialog.addEventListener('click', event => { const bounds = dialog.getBoundingClientRect(); if (event.target === dialog && (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom)) dialog.close(); }));
function updateProfile() {
  $('#profile-name').textContent = state.name || '名前を設定'; $('#profile-avatar').textContent = state.name ? [...state.name][0] : 'G';
}
function openProfile() { $('#profile-form input').value = state.name; $('#profile-dialog').showModal(); }
$('#profile-dialog').addEventListener('close', () => { if (state.view === 'mine' && !state.name) { state.view = 'all'; render(); } });
$('#profile-button').addEventListener('click', openProfile); $('#mobile-profile').addEventListener('click', openProfile);
$('#profile-form').addEventListener('submit', event => { event.preventDefault(); const name = $('#profile-form input').value.trim(); if (!name) return; state.name = name; storage.set('name', name); updateProfile(); $('#profile-dialog').close(); render(); notify('表示名を保存しました'); });
$('#help-button').addEventListener('click', () => $('#help-dialog').showModal());
$$('[data-view]').forEach(button => button.addEventListener('click', () => { state.view = button.dataset.view; if (state.view === 'mine' && !state.name) openProfile(); render(); }));
$('#search').addEventListener('input', event => { state.search = event.target.value.trim().toLocaleLowerCase(); render(); });
for (const [id, field] of [['category-filter', 'category'], ['assignee-filter', 'assignee'], ['priority-filter', 'priority'], ['sort', 'sort']]) $(`#${id}`).addEventListener('change', event => { state[field] = event.target.value; render(); });
$('#clear-filters').addEventListener('click', () => { state.category = 'all'; state.assignee = 'all'; state.priority = 'all'; state.search = ''; $('#category-filter').value = 'all'; $('#assignee-filter').value = 'all'; $('#priority-filter').value = 'all'; $('#search').value = ''; render(); });
for (const layout of ['board', 'list']) $(`#view-${layout}`).addEventListener('click', () => { state.layout = layout; storage.set('layout', layout); if (layout === 'board' && state.view === 'archived') state.view = 'all'; render(); });
$('#load-samples').addEventListener('click', async () => { const button = $('#load-samples'); button.disabled = true; try { await api('/api/samples', { method: 'POST' }); await load(); notify('サンプルタスクを追加しました'); } catch (error) { notify(error.message); } finally { button.disabled = false; } });
$('#archive-samples').addEventListener('click', async () => { const button = $('#archive-samples'); button.disabled = true; try { await api('/api/samples/archive', { method: 'POST' }); await load(); notify('サンプルをアーカイブしました。いつでも復元できます。'); } catch (error) { notify(error.message); } finally { button.disabled = false; } });
$('#refresh').addEventListener('click', () => load());
window.addEventListener('focus', () => { if (!$('#workspace').hidden && !hasDialog() && !busy.size) load({ silent: true }); });
setInterval(() => { if (!document.hidden && !$('#workspace').hidden && !hasDialog() && !busy.size) load({ silent: true }); }, 30000);
boot();
