import { Api } from "./api.js";
import { Window } from "./components.js";
import { document, redom } from "./externals.js";

const { mount } = redom;

const api = new Api()
const w = new Window(api);
mount(document.body, w);

window.w = w;
