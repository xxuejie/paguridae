import { redom, Quill } from "./externals.js";

const {el, listPool, setChildren, setStyle} = window.redom;
const Delta = Quill.import("delta");

const BUTTONS = {
  LEFT: 0,
  MIDDLE: 2,
  RIGHT: 4
};
Object.freeze(BUTTONS);

export class Window {
  constructor() {
    this.el = el(".window");
    this.rows = listPool(Row, "id");

    this.el.addEventListener("click", function(event) {
      let button = BUTTONS.LEFT;
      if (event.button === 0 && event.altKey) {
        button = BUTTONS.MIDDLE;
      }
      if (button !== BUTTONS.LEFT) {
        this.testForAction(event.target, button);
      }
    }.bind(this));
    this.el.addEventListener("contextmenu", function(event) {
      event.preventDefault();
      this.testForAction(event.target, BUTTONS.RIGHT);
    }.bind(this));
  }

  testForAction(target, button) {
    while (target != this.el.el && (!target.onaction) && target.parentElement) {
      target = target.parentElement;
    }
    if (target.onaction) {
      target.onaction(button);
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

    (changes || []).forEach(function(change) {
      const row = lookup[change.id];
      if (row) {
        row.update(change);
      }
    });
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

    this.labelEditor.on("selection-change", function(range, oldRange) {
      console.log(`Row ${this.id} label change from: `, oldRange, " to: ", range);
      console.log("Saved range: ", this.labelEditor.selection.savedRange);
    }.bind(this));
    this.contentEditor.on("selection-change", function(range, oldRange) {
      console.log(`Row ${this.id} content change from: `, oldRange, " to: ", range);
      console.log("Saved range: ", this.contentEditor.selection.savedRange);
    }.bind(this));

    this.contentEditor.on("text-change", function(delta) {
      console.log("Editor content change: ", delta);
    });

    this.label.onaction = function(button) {
      console.log("label on trigger: ", button);
      console.log("Selection: ", this.labelEditor.getSelection());
    }.bind(this);
    this.content.onaction = function(button) {
      console.log("content on trigger: ", button);
      console.log("Selection: ", this.contentEditor.getSelection());
    }.bind(this);
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
