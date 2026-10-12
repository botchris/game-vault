// The extension's menu: the Game Vault addresses enabled for sign-ins, and the store-page option.
import { bridgeId, enableOrigin } from './lib/bridges.js';
import { STORE_ORIGINS, grantedStores, removableOrigins, tokenInClear } from './lib/ownership.js';

const $ = (id) => document.getElementById(id);
const load = async (k, d) => (await chrome.storage.local.get(k))[k] ?? d;
const t = (key, subs) => chrome.i18n.getMessage(key, subs);

// Static texts carry the name of their message.
for (const el of document.querySelectorAll('[data-i18n]')) el.textContent = t(el.dataset.i18n);

async function saveOwnership(patch) {
  const ownership = await load('ownership', {});
  await chrome.storage.local.set({ ownership: { ...ownership, ...patch } });
}

async function render() {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  const origins = await load('origins', []);
  const grants = await load('grants', {});
  const current = $('current');
  current.textContent = '';
  let here = '';
  try { here = new URL(tab?.url ?? '').origin; } catch { /* not a web page */ }
  if (/^https?:/.test(here) && !origins.includes(here)) {
    const b = document.createElement('button');
    b.textContent = t('popupEnableOn', [here]);
    b.onclick = async () => {
      const { protocol, hostname } = new URL(here);
      // Firefox closes this popup for the permission prompt: the background finishes from here.
      await chrome.storage.local.set({ pendingEnable: { origin: here, tabId: tab.id } });
      if (!await chrome.permissions.request({ origins: [`${protocol}//${hostname}/*`] })) {
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
  } else if (origins.includes(here)) {
    current.textContent = t('popupEnabledHere');
  }

  const list = $('origins');
  list.textContent = '';
  for (const origin of origins) {
    const li = document.createElement('li');
    const remove = document.createElement('button');
    remove.textContent = t('popupRemove');
    remove.onclick = async () => {
      await chrome.scripting.unregisterContentScripts({ ids: [bridgeId(origin)] }).catch(() => {});
      delete grants[origin];
      await chrome.storage.local.set({ origins: origins.filter((o) => o !== origin), grants });
      render();
    };
    li.append(origin, ' ', remove);
    const hosts = grants[origin] ?? [];
    if (hosts.length) {
      const ul = document.createElement('ul');
      for (const h of hosts) {
        const hl = document.createElement('li');
        const x = document.createElement('button');
        x.textContent = '×';
        x.title = t('popupForget', [h]);
        x.onclick = async () => {
          grants[origin] = hosts.filter((o) => o !== h);
          await chrome.storage.local.set({ grants });
          render();
        };
        hl.append(h, ' ', x);
        ul.append(hl);
      }
      li.append(ul);
    }
    list.append(li);
  }
  if (!origins.length) {
    const li = document.createElement('li');
    li.className = 'muted';
    li.textContent = t('popupNoAddresses');
    list.append(li);
  }
  $('version').textContent = t('popupVersion', [chrome.runtime.getManifest().version]);
  await renderOwnership(origins);
}

async function renderOwnership(origins) {
  const ownership = await load('ownership', {});
  const select = $('own-origin');
  select.textContent = '';
  for (const o of origins) {
    const opt = document.createElement('option');
    opt.value = o;
    opt.textContent = o;
    select.append(opt);
  }
  const origin = origins.includes(ownership.origin) ? ownership.origin : origins[0] ?? '';
  select.value = origin;
  select.disabled = !origins.length;
  $('own-enabled').checked = Boolean(ownership.enabled);
  $('own-enabled').disabled = !origins.length;
  $('own-token').value = ownership.token ?? '';
  // Plain http off this computer: anyone on the network could read the token on its way.
  $('own-insecure').hidden = !tokenInClear(origin);
  if (origin && origin !== ownership.origin) await saveOwnership({ origin });
  const stores = await grantedStores(chrome);
  $('own-status').textContent = !origins.length ? t('ownershipNoAddress')
    : !ownership.enabled ? ''
      : stores.length ? t('ownershipStores', [stores.join(', ')]) : t('ownershipNoStores');
}

$('own-origin').onchange = async (e) => {
  await saveOwnership({ origin: e.target.value });
  render();
};
$('own-token').onchange = (e) => saveOwnership({ token: e.target.value.trim() });
$('own-enabled').onchange = async (e) => {
  if (e.target.checked) {
    // Saved first: Firefox closes this popup for the permission prompt, and the background
    // registers the store scripts once the browser grants them.
    await saveOwnership({ enabled: true });
    if (!await chrome.permissions.request({ origins: STORE_ORIGINS })) await saveOwnership({ enabled: false });
  } else {
    await saveOwnership({ enabled: false });
    const origins = removableOrigins(await load('grants', {}));
    if (origins.length) await chrome.permissions.remove({ origins }).catch(() => {});
  }
  render();
};

render();
