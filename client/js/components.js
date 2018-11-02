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

    this.el.addEventListener("mouseup", event => {
      let button = event.button;
      if (button === 0 && event.altKey) {
        button = 1;
      }
      const action = ACTIONS[button];
      if (action) {
        this.testForAction(event.target, action);
      }
    });
    this.el.addEventListener("contextmenu", event => {
      event.preventDefault();
    });
  }

  testForAction(target, type) {
    while (target != this.el && (!target.onaction) && target.parentElement) {
      target = target.parentElement;
    }
    if (target.onaction) {
      target.onaction(type);
    }
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
  constructor(api, { id }) {
    this.api = api;
    this.id = id;
    this.label = el(".label");
    this.content = el(".content");
    this.el = el(".row", [this.label, this.content]);

    this.labelEditor = new Quill(this.label);
    this.contentEditor = new Quill(this.content);

    this.label.onaction = action => {
      this.onaction(action, this.labelEditor, "label");
    };
    this.content.onaction = action => {
      this.onaction(action, this.contentEditor, "context");
    };
  }

  onaction(action, editor, type) {
    let selection = editor.getSelection();
    if (selection.length === 0) {
      // expand selection around cursor
      const {index} = selection;
      const sliceStart = Math.max(index - 100, 0);
      const sliceEnd = Math.min(index + 100, editor.getLength());
      const text1 = editor.getText(sliceStart, index);
      const text2 = editor.getText(index, sliceEnd);
      let text1SpaceIndex = -1;
      for (const [i, ch] of text1.split("").entries()) {
        if (WHITESPACE.test(ch)) {
          text1SpaceIndex = i;
        }
      }
      const selectionStart = text1SpaceIndex + 1 + sliceStart;
      let text2SpaceIndex = -1;
      for (const [i, ch] of text2.split("").entries()) {
        if (WHITESPACE.test(ch)) {
          text2SpaceIndex = i;
          break;
        }
      }
      let selectionEnd = sliceEnd;
      if (text2SpaceIndex >= 0) {
        selectionEnd = text2SpaceIndex + index;
      }
      selection = {
        index: selectionStart,
        length: selectionEnd - selectionStart
      };
    }
    console.log(`Selecting: "${editor.getText(selection.index, selection.length)}"`);
    this.api[action]({
      id: this.id,
      type,
      selection
    });
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
