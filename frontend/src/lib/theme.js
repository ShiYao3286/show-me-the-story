// UI theme (dark / light). Defaults to dark, persisted to localStorage.
//
// Both themes are daisyUI themes registered by @xianii/design-system:
//   "xianii"        — dark, normative
//   "xianii-light"  — light; app.css overrides its tokens with the paper palette.
// Switching only flips <html data-theme>, so every component follows without changes.
import { writable, get } from 'svelte/store';

const STORAGE_KEY = 'showmethestory.uiTheme';

export const THEME_DARK = 'xianii';
export const THEME_LIGHT = 'xianii-light';

const THEMES = [THEME_DARK, THEME_LIGHT];

function readStored() {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    if (THEMES.includes(v)) return v;
  } catch (e) {}
  // Respect the OS preference on first run.
  try {
    if (window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches) {
      return THEME_LIGHT;
    }
  } catch (e) {}
  return THEME_DARK;
}

export const uiTheme = writable(readStored());

uiTheme.subscribe(v => {
  const theme = THEMES.includes(v) ? v : THEME_DARK;
  try {
    localStorage.setItem(STORAGE_KEY, theme);
  } catch (e) {}
  if (typeof document !== 'undefined') {
    document.documentElement.setAttribute('data-theme', theme);
  }
});

export function isLightTheme(theme) {
  return theme === THEME_LIGHT;
}

export function toggleTheme() {
  uiTheme.update(v => (isLightTheme(v) ? THEME_DARK : THEME_LIGHT));
}

export function getTheme() {
  return get(uiTheme);
}
