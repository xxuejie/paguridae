import { fill } from "./space.js";
import { redom, Quill } from "./externals.js";

const {el, list, setStyle} = window.redom;
const Delta = Quill.import("delta");

export class Window {
  constructor() {
    this.el = list(".window", Column, "id");
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
  }

  update({height, label, content}) {
    setStyle(this.el, {height: `${height}%`});
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
