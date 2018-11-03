import { redom, Quill } from "./externals.js";

const {el, listPool, setChildren, setStyle} = window.redom;
const Delta = Quill.import("delta");

const WHITESPACE = /^\s$/;
const ACTIONS = {
  // Middle click
  1: "execute",
  // Right click
  2: "search"
};
Object.freeze(ACTIONS);

export class Window {
  constructor(api) {
    this.el = el(".window");
    this.rows = listPool(Row, "id", api);

    api.init(data => {
      this.update(data);
    });

    this.el.addEventListener("mousedown", event => {
      const action = this.extractAction(event);
      if (!action) { return ;}
      const editor = this.findEditor(event);
      this.mousedownSelection = editor.__row.selection(editor.__type);
    });
    this.el.addEventListener("mouseup", event => {
      const action = this.extractAction(event);
      if (!action) { return ;}
      const editor = this.findEditor(event);
      const selection = editor.__row.selection(editor.__type);
      // When mouseup lands on a different editor from mousedown, selection
      // would return null.
      if (!selection) { return; }

      // When selection in current mouseup event matches selection from
      // mousedown event, we will do nothing to the selection since this is
      // a user specified selection. However, if 2 selections do not match,
      // it must be browser that's filling selection for us, we would reset
      // selection length to 0, and only keep cursor position so we can properly
      // expand selection at backend. Notice it might happen that cursor is
      // in the middle of a word, but in the reset process, we are only
      // resetting the cursor to the start of the word. Based on current
      // selection logic in the browser and backend, this won't affect anything.
      if ((!this.mousedownSelection) ||
          (selection.index !== this.mousedownSelection.index &&
           selection.length !== this.mousedownSelection.length)) {
        selection.length = 0;
      }

      api[action]({
        id: editor.__row.id,
        type: editor.__type,
        selection,
      });
    });
    this.el.addEventListener("contextmenu", event => {
      event.preventDefault();
      return false;
    });
  }

  extractAction(event) {
    let button = event.button;
    if (button === 0 && event.altKey) {
      button = 1;
    }
    return ACTIONS[button];
  }

  findEditor(event) {
    let target = event.target;
    while (target != this.el && (!target.__row) && target.parentElement) {
      target = target.parentElement;
    }
    return target;
  }

  update({layout, changes}) {
    if (layout) {
      const rowData = [].concat(...layout.columns.map(function({rows}) {
        return rows;
      }));
      this.rows.update(rowData);
      const { lookup } = this.rows;

      const columnEls = layout.columns.map(function({rows, width}) {
        const columnEl = el(".column");
        setStyle(columnEl, {width: `${width}%`});

        const rowEls = rows.map(function({id}) {
          return lookup[id];
        });

        setChildren(columnEl, rowEls);
        return columnEl;
      });
      setChildren(this.el, columnEls);
    }

    if (changes) {
      const { lookup } = this.rows;
      changes.forEach(function(change) {
        const row = lookup[change.id];
        if (row) {
          row.update(change);
        }
      });
    }
  }
}

export class Row {
  constructor(_initData, { id }) {
    this.id = id;
    this.label = el(".label");
    this.content = el(".content");
    this.el = el(".row", [this.label, this.content]);

    this.labelEditor = new Quill(this.label);
    this.contentEditor = new Quill(this.content);

    this.label.__row = this;
    this.content.__row = this;

    this.label.__type = "label";
    this.content.__type = "content";
  }

  selection(type) {
    if (type === "label") {
      return this.labelEditor.getSelection();
    } else if (type === "content") {
      return this.contentEditor.getSelection();
    }
    return null;
  }

  update({height, label, content}) {
    if (height) {
      setStyle(this.el, {height: `${height}%`});
    }
    if (label) {
      this.labelEditor.updateContents(new Delta(label));
    }
    if (content) {
      this.contentEditor.updateContents(new Delta(content));
    }
  }
}
