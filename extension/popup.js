// The extension's menu: the Game Vault addresses enabled for sign-ins, and the store-page option.
import { bridgeId, enableOrigin } from './lib/bridges.js';
import { saveAndRequest } from './lib/permission.js';
import {
  STORE_ORIGINS, activeSettings, grantedStores, nextOwnership, popupPlace, removableOrigins, storeForUrl, storeOrigin, storePageMessage, tokenInClear,
} from './lib/ownership.js';

const $ = (id) => document.getElementById(id);
const load = async (k, d) => (await chrome.storage.local.get(k))[k] ?? d;
const t = (key, subs) => chrome.i18n.getMessage(key, subs);

// Static texts carry the name of their message.
for (const el of document.querySelectorAll('[data-i18n]')) el.textContent = t(el.dataset.i18n);

// The store-page option as the popup shows it. Every change is saved from it, whole, without
// reading storage first: the writes then land in the order of the clicks (a token saved on blur
// cannot overwrite the option turned on by the click that caused the blur), and turning the option
// on needs no await before the permission request (see saveAndRequest).
let shownOwnership = {};

function saveOwnership(patch) {
  shownOwnership = { ...shownOwnership, ...patch };
  return chrome.storage.local.set({ ownership: shownOwnership });
}

async function render() {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  const origins = await load('origins', []);
  const grants = await load('grants', {});
  const current = $('current');
  current.textContent = '';
  const place = popupPlace(tab?.url ?? '', origins, grants);
  current.dataset.kind = place.kind;
  if (place.kind === 'offer') {
    const here = place.origin;
    const b = document.createElement('button');
    b.id = 'enable-here';
    b.textContent = t('popupEnableOn', [here]);
    b.onclick = async () => {
      const { protocol, hostname } = new URL(here);
      // Firefox closes this popup for the permission prompt: the background finishes from here.
      // Nothing may be awaited before the request (see saveAndRequest).
      const pendingEnable = { origin: here, tabId: tab.id };
      if (!await saveAndRequest(chrome, { pendingEnable }, { origins: [`${protocol}//${hostname}/*`] })) {
        await chrome.storage.local.remove('pendingEnable');
        return;
      }
      await chrome.storage.local.remove('pendingEnable');
      await enableOrigin(here, tab.id).catch(() => {}); // the background may be finishing it too
      render();
    };
    const p = document.createElement('p');
    p.className = 'muted';
    p.textContent = t('popupEnableHelp');
    current.append(b, p);
  } else if (place.kind === 'enabled') {
    current.textContent = t('popupEnabledHere');
  } else if (place.kind === 'store') {
    // A store page is never a Game Vault: what it gets is the labels, set below.
    const on = (await activeSettings(chrome.storage.local)) !== null;
    const granted = await chrome.permissions.contains({ origins: [storeOrigin(storeForUrl(tab.url))] });
    const { key, subs } = storePageMessage(place.store, { on, granted });
    current.textContent = t(key, subs);
  }

  const list = $('origins');
  list.textContent = '';
  for (const origin of origins) {
    const li = document.createElement('li');
    const head = document.createElement('div');
    head.className = 'origin';
    // The host stands out; the scheme and port are there, quieter, since they tell addresses apart.
    const { protocol, host } = new URL(origin);
    const name = document.createElement('span');
    name.className = 'origin-name';
    name.title = origin;
    const scheme = document.createElement('span');
    scheme.textContent = `${protocol}//`;
    name.append(scheme, host);
    const remove = document.createElement('button');
    remove.textContent = t('popupRemove');
    remove.onclick = async () => {
      await chrome.scripting.unregisterContentScripts({ ids: [bridgeId(origin)] }).catch(() => {});
      delete grants[origin];
      await chrome.storage.local.set({ origins: origins.filter((o) => o !== origin), grants });
      render();
    };
    head.append(name, remove);
    li.append(head);
    const hosts = grants[origin] ?? [];
    if (hosts.length) {
      const ul = document.createElement('ul');
      ul.className = 'chips';
      for (const h of hosts) {
        const hl = document.createElement('li');
        hl.className = 'chip';
        const x = document.createElement('button');
        x.textContent = '×';
        x.title = t('popupForget', [h]);
        x.setAttribute('aria-label', x.title);
        x.onclick = async () => {
          grants[origin] = hosts.filter((o) => o !== h);
          await chrome.storage.local.set({ grants });
          render();
        };
        hl.append(h, x);
        ul.append(hl);
      }
      li.append(ul);
    }
    list.append(li);
  }
  if (!origins.length) {
    const li = document.createElement('li');
    li.className = 'empty';
    li.textContent = t('popupNoAddresses');
    list.append(li);
  }
  $('version').textContent = t('popupVersion', [chrome.runtime.getManifest().version]);
  await renderOwnership(origins);
}

/** Moves the option to another address: the token is forgotten and the option turned off (see nextOwnership). */
async function chooseOrigin(origin) {
  const saved = await load('ownership', undefined);
  const ownership = nextOwnership(saved, origin);
  if (ownership !== saved) await chrome.storage.local.set({ ownership });
  return ownership;
}

async function renderOwnership(origins) {
  const saved = await load('ownership', {});
  // The saved address was removed: another one (or none) is chosen, without the old token.
  const ownership = await chooseOrigin(origins.includes(saved.origin) ? saved.origin : origins[0] ?? '');
  shownOwnership = ownership;
  const { origin } = ownership;
  const select = $('own-origin');
  select.textContent = '';
  for (const o of origins) {
    const opt = document.createElement('option');
    opt.value = o;
    opt.textContent = o;
    select.append(opt);
  }
  select.value = origin;
  select.disabled = !origins.length;
  $('own-enabled').checked = Boolean(ownership.enabled);
  $('own-enabled').disabled = !origins.length;
  $('own-token').value = ownership.token ?? '';
  // Plain http off this computer: anyone on the network could read the token on its way.
  $('own-insecure').hidden = !tokenInClear(origin);
  const stores = await grantedStores(chrome);
  $('own-status').textContent = !origins.length ? t('ownershipNoAddress')
    : !ownership.enabled ? ''
      : stores.length ? t('ownershipStores', [stores.join(', ')]) : t('ownershipNoStores');
}

$('own-origin').onchange = async (e) => {
  await chooseOrigin(e.target.value);
  render();
};
$('own-token').onchange = (e) => saveOwnership({ token: e.target.value.trim() });
$('own-enabled').onchange = async (e) => {
  if (e.target.checked) {
    // Saved with the request: Firefox closes this popup for the permission prompt, and the
    // background registers the store scripts once the browser grants them.
    shownOwnership = { ...shownOwnership, enabled: true };
    if (!await saveAndRequest(chrome, { ownership: shownOwnership }, { origins: STORE_ORIGINS })) await saveOwnership({ enabled: false });
  } else {
    await saveOwnership({ enabled: false });
    const origins = removableOrigins(await load('grants', {}));
    if (origins.length) await chrome.permissions.remove({ origins }).catch(() => {});
  }
  render();
};

render();
