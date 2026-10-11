import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import App from './App';
import '@fontsource-variable/archivo/wdth.css';
import './i18n';
import './styles.css';

// Covers once kept their band colours here; the backdrop is now the blurred cover, so drop them.
try { localStorage.removeItem('gamevault.coverColors'); } catch { /* storage blocked: nothing kept */ }

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
