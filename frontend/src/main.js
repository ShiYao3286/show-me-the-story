import App from './App.svelte';
import './app.css';
// Import for side effect: applies the persisted theme to <html data-theme> before mount,
// so there is no flash of the wrong theme on load.
import './lib/theme.js';

const app = new App({
  target: document.getElementById('app')
});

export default app;
