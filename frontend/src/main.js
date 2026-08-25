// Vite-Entrypoint: Bootstrap laden und die Svelte-5-App mounten.
// ZUERST: Pfad-Deep-Links in Hash-Routen übersetzen, bevor der Router lädt.
import './lib/deepLink.js';
import 'bootstrap/dist/css/bootstrap.min.css';
// LCM-Design-System NACH Bootstrap: verfeinert Dark Mode, Elevation, Abstände.
import './styles/theme.css';
import { mount } from 'svelte';
import App from './App.svelte';

const app = mount(App, { target: document.getElementById('app') });

export default app;
