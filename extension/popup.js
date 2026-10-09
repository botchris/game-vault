import { registerBridge, bridgeId } from './lib/bridges.js';

const $ = (id) => document.getElementById(id);
const load = async (k, d) => (await chrome.storage.local.get(k))[k] ?? d;

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
    b.textContent = `Enable on ${here}`;
    b.onclick = async () => {
      const { protocol, hostname } = new URL(here);
      if (!await chrome.permissions.request({ origins: [`${protocol}//${hostname}/*`] })) return;
      await chrome.storage.local.set({ origins: [...origins, here] });
      await registerBridge(here);
      await chrome.scripting.executeScript({ target: { tabId: tab.id }, files: ['bridge.js'] });
      render();
    };
    const p = document.createElement('p');
    p.className = 'muted';
    p.textContent = 'Only on your own Game Vault: this lets that address ask the extension to connect stores.';
    current.append(b, p);
  } else if (origins.includes(here)) {
    current.textContent = `Enabled on this address. Use Connect in a source's settings.`;
  }

  const list = $('origins');
  list.textContent = '';
  for (const origin of origins) {
    const li = document.createElement('li');
    const remove = document.createElement('button');
    remove.textContent = 'Remove';
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
        x.title = `Forget ${h}`;
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
  if (!origins.length) list.innerHTML = '<li class="muted">None yet: open your Game Vault and press Enable.</li>';
  $('version').textContent = `Version ${chrome.runtime.getManifest().version}`;
}

render();
