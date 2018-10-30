import { fill } from "./layout.js";
import { redom, Quill } from "./externals.js";

const {el, list, setStyle} = window.redom;
const Delta = Quill.import("delta");

const BUTTONS = {
  LEFT: 0,
  MIDDLE: 2,
  RIGHT: 4
};
Object.freeze(BUTTONS);

export class Window {
  constructor() {
    this.el = list(".window", Column, "id");

    this.el.el.addEventListener("click", function(event) {
      let button = BUTTONS.LEFT;
      if (event.button === 0 && event.altKey) {
        button = BUTTONS.MIDDLE;
      }
      if (button !== BUTTONS.LEFT) {
        this.testForAction(event.target, button);
      }
    }.bind(this));
    this.el.el.addEventListener("contextmenu", function(event) {
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

  update({columns}) {
    const columnsWithWidths = fill(columns, "width");
    this.el.update(columnsWithWidths);
  }
}

export class Column {
  constructor(_initData, { id }) {
    this.id = id;
    this.el = list(".column", Row, "id");
  }

  update({rows, width}) {
    setStyle(this.el, {width: `${width}%`});
    const rowsWithHeights = fill(rows, "height");
    this.el.update(rowsWithHeights);
  }
}

export class Row {
  constructor(_initData, { id }) {
    this.id = id;
    this.label = el(".label");
    this.content = el(".content");
    this.el = el(".row", [this.label, this.content]);

    this.label.onaction = function(button) {
      console.log("label on trigger: ", button);
      console.log("Selection: ", this.labelEditor.getSelection());
    }.bind(this);
    this.content.onaction = function(button) {
      console.log("content on trigger: ", button);
      console.log("Selection: ", this.contentEditor.getSelection());
    }.bind(this);
  }

  onmount() {
    this.labelEditor = new Quill(this.label);
    if (this.labelContents) {
      this.labelEditor.updateContents(this.labelContents);
      this.labelContents = null;
    }
    this.contentEditor = new Quill(this.content);
    if (this.contentContents) {
      this.contentEditor.updateContents(this.contentContents);
      this.contentContents = null;
    }

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
  }

  update({height, label, content}) {
    if (height) {
      setStyle(this.el, {height: `${height}%`});
    }
    if (label) {
      this.labelContents = new Delta(label);
      if (this.labelEditor) {
        this.labelEditor.updateContents(this.labelContents);
        this.labelContents = null;
      }
    }
    if (content) {
      this.contentContents = new Delta(content);
      if (this.contentEditor) {
        this.contentEditor.updateContents(this.contentContents);
        this.contentContents = null;
      }
    }
  }
}
