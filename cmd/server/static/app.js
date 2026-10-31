'use strict';
const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];
const state = { ideas: [], category: 'all', view: 'all', search: '', sort: 'popular' };
const names = { idea: 'アイデア', building: 'つくっている', shipped: 'できた！' };
let toastTimer;
let requestNumber = 0;
function notify(message) {
  clearTimeout(toastTimer);
  $('#toast').textContent = message;
  $('#toast').hidden = false;
  toastTimer = setTimeout(() => { $('#toast').hidden = true; }, 3500);
}
async function api(path, options = {}) {
  const response = await fetch(path, { ...options, headers: { 'Content-Type': 'application/json', ...options.headers } });
  let data;
  try { data = await response.json(); } catch { throw new Error('サーバーから応答を受け取れませんでした。'); }
  if (!response.ok) throw new Error(data.error || '通信に失敗しました。もう一度お試しください。');
  return data;
}
async function load({ silent = false } = {}) {
  const number = ++requestNumber;
  $('#refresh').disabled = true;
  try {
    const ideas = await api('/api/ideas');
    if (number !== requestNumber) return;
    state.ideas = ideas;
    $('#load-error').hidden = true;
    render();
  } catch (error) {
    if (number !== requestNumber) return;
    $('#load-error').textContent = error.message + ' 右上の ↻ から再試行できます。';
    $('#load-error').hidden = false;
    if (!state.ideas.length) $$('.cards').forEach(cards => { cards.textContent = ''; const empty = document.createElement('div'); empty.className = 'empty'; empty.textContent = 'データを取得できませんでした'; cards.append(empty); });
    if (!silent) notify(error.message);
  } finally {
    if (number === requestNumber) { $('#refresh').disabled = false; $('#board').setAttribute('aria-busy', 'false'); }
  }
}
function render() {
  $('#stat-ideas').textContent = state.ideas.length;
  $('#nav-total').textContent = state.ideas.length;
  $('#stat-votes').textContent = state.ideas.reduce((total, idea) => total + idea.votes, 0).toLocaleString();
  $('#stat-shipped').textContent = state.ideas.filter(idea => idea.status === 'shipped').length;
  const filtered = state.ideas.filter(idea =>
    (state.category === 'all' || idea.category === state.category) &&
    (state.view !== 'voted' || idea.voted) &&
    (state.view !== 'shipped' || idea.status === 'shipped') &&
    (!state.search || `${idea.title} ${idea.description} ${idea.author}`.toLocaleLowerCase().includes(state.search))
  ).sort((a, b) => state.sort === 'popular' ? b.votes - a.votes || b.id - a.id : new Date(b.createdAt) - new Date(a.createdAt) || b.id - a.id);
  for (const column of $$('[data-column]')) {
    const status = column.dataset.column;
    const ideas = filtered.filter(idea => idea.status === status);
    $('.column-count', column).textContent = ideas.length;
    const cards = $('.cards', column);
    cards.replaceChildren(...ideas.map(card));
    if (!ideas.length) {
      const empty = document.createElement('div'); empty.className = 'empty';
      empty.textContent = state.view === 'voted' ? '気になるアイデアに投票してみよう' : state.search || state.category !== 'all' ? '一致するアイデアがありません' : 'ここから、新しい一歩を。';
      cards.append(empty);
    }
    if (status === 'idea' && state.view === 'all') {
      const add = document.createElement('button'); add.type = 'button'; add.className = 'column-add'; add.textContent = '＋ 新しいアイデア'; add.addEventListener('click', openDialog); cards.append(add);
    }
  }
}
function card(idea) {
  const fragment = $('#card-template').content.cloneNode(true);
  const article = $('article', fragment);
  article.dataset.id = idea.id;
  $('.category-label', fragment).textContent = idea.category;
  $('.category-label', fragment).classList.add(idea.category);
  $('.card-id', fragment).textContent = `LB–${String(idea.id).padStart(3, '0')}`;
  $('h4', fragment).textContent = idea.title;
  $('.card-description', fragment).textContent = idea.description;
  $('.author-name', fragment).textContent = idea.author;
  $('.avatar', fragment).textContent = [...idea.author][0].toUpperCase();
  const vote = $('.vote', fragment);
  vote.classList.toggle('voted', idea.voted);
  vote.setAttribute('aria-pressed', String(idea.voted));
  vote.setAttribute('aria-label', `${idea.title} に${idea.voted ? '投票を取り消す' : '投票する'} (${idea.votes}票)`);
  $('strong', vote).textContent = idea.votes;
  vote.addEventListener('click', async () => {
    vote.disabled = true;
    try { await api(`/api/ideas/${idea.id}/vote`, { method: 'POST', body: JSON.stringify({ voted: !idea.voted }) }); await load(); }
    catch (error) { notify(error.message); }
    finally { vote.disabled = false; }
  });
  const select = $('.card-status select', fragment);
  select.value = idea.status;
  select.setAttribute('aria-label', `${idea.title} のステータスを変更`);
  select.addEventListener('change', async () => {
    select.disabled = true;
    try { await api(`/api/ideas/${idea.id}`, { method: 'PATCH', body: JSON.stringify({ status: select.value }) }); notify(`「${names[select.value]}」に移動しました`); await load(); }
    catch (error) { select.value = idea.status; notify(error.message); }
    finally { select.disabled = false; }
  });
  return fragment;
}
const dialog = $('#idea-dialog');
function openDialog() { $('#form-error').textContent = ''; if (!dialog.open) dialog.showModal(); }
$$('[data-create]').forEach(button => button.addEventListener('click', openDialog));
$('#close-dialog').addEventListener('click', () => dialog.close());
dialog.addEventListener('click', event => { const bounds = dialog.getBoundingClientRect(); if (event.target === dialog && (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom)) dialog.close(); });
$('#idea-form').addEventListener('submit', async event => {
  event.preventDefault();
  const button = $('#submit-idea'); button.disabled = true; button.textContent = '届けています…'; $('#form-error').textContent = '';
  try {
    const fields = Object.fromEntries(new FormData(event.currentTarget));
    await api('/api/ideas', { method: 'POST', body: JSON.stringify(fields) });
    $('#idea-form').reset(); dialog.close();
    state.category = 'all'; state.view = 'all'; state.search = ''; state.sort = 'recent';
    $('#search').value = ''; $('#sort').value = 'recent'; updateNavigation(); updateCategories();
    await load(); notify('新しいアイデアが、ボードに加わりました！');
  } catch (error) { $('#form-error').textContent = error.message; }
  finally { button.disabled = false; button.textContent = 'アイデアを届ける ↗'; }
});
function updateNavigation() {
  $$('[data-view]').forEach(button => button.classList.toggle('active', button.dataset.view === state.view));
  $('#page-name').textContent = { all: 'Overview', voted: 'My votes', shipped: 'Shipped' }[state.view];
  $('#board-title').textContent = { all: 'アイデアボード', voted: 'あなたが応援したアイデア', shipped: 'かたちになったアイデア' }[state.view];
}
function updateCategories() {
  $$('[data-category]').forEach(button => { const selected = button.dataset.category === state.category; button.classList.toggle('selected', selected); button.setAttribute('aria-pressed', String(selected)); });
}
$$('[data-category]').forEach(button => button.addEventListener('click', () => { state.category = button.dataset.category; updateCategories(); render(); }));
$$('[data-view]').forEach(button => button.addEventListener('click', () => { state.view = button.dataset.view; updateNavigation(); render(); }));
$('#search').addEventListener('input', event => { state.search = event.target.value.trim().toLocaleLowerCase(); render(); });
$('#sort').addEventListener('change', event => { state.sort = event.target.value; render(); });
$('#refresh').addEventListener('click', () => load());
window.addEventListener('focus', () => { if (!dialog.open) load({ silent: true }); });
setInterval(() => { if (!document.hidden && !dialog.open) load({ silent: true }); }, 30000);
load();
