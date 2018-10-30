import { Api } from "./api.js";
import { Window } from "./components.js";
import { document, redom } from "./externals.js";

const { mount } = redom;

const w = new Window();
mount(document.body, w);

const api = new Api(function(data) {
  w.update(data);
});

window.w = w;
