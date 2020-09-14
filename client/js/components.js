import {
  clipboard, document, redom, verifyContent,
  addEventListener, getComputedStyle, windowDimension,
  Quill, IS_MOBILE
} from "./externals.js";

const {el, listPool, setChildren, setStyle} = redom;
const Delta = Quill.import("delta");

const ACTIONS = {
  // Middle click
  1: "execute",
  // Right click
  2: "search"
};
Object.freeze(ACTIONS);

const ACTION_SELECTION_EXPAND_LENGTH = 256;
function generate_action(action, editor, { index, length }, api, oldSelection) {
  if (!oldSelection) {
    // Use current command selection in case old selection does not exist
    oldSelection = {
      id: editor.__id,
      range: { index, length }
    };
  }
  let command = "";
  if (length > 0) {
    command = editor.__quill.getText(index, length);
  } else {
    const start = (index > ACTION_SELECTION_EXPAND_LENGTH) ?
                  (index - ACTION_SELECTION_EXPAND_LENGTH) :
                  0;
    const firstHalf = editor.__quill.getText(start, index - start);
    const secondHalf = editor.__quill.getText(index, ACTION_SELECTION_EXPAND_LENGTH);
    const firstHalfMatch = (firstHalf.match(/\S+$/) || [""])[0];
    const secondHalfMatch = (secondHalf.match(/^\S+/) || [""])[0];
    command = `${firstHalfMatch}${secondHalfMatch}`;
  }
  api.action({
    type: action,
    id: editor.__id,
    index,
    command,
    selection: oldSelection
  });
}

export class Window {
  constructor(api) {
    this.api = api;
    this.el = el(".window");
    this.rows = listPool(Row, "id", { api, root: this });
    this.currentSelection = null;
    this.leftButtonDown = false;
    this.chordProcessed = false;
    this.layout = new Layout();

    addEventListener("resize", () => {
      this.updateEditorSizes();
    });

    api.init(this.layout, data => {
      this.update(data);
    }, hashes => {
      this.verify(hashes)
    });

    this.el.addEventListener("mousedown", event => {
      const editor = this.findEditor(event);
      if ((!editor) || (!editor.__quill)) { return; }
      if (this.leftButtonDown) {
        if (event.button === 1 || (event.button === 2 && event.ctrlKey)) {
          const selection = editor.__quill.getSelection();
          if (selection) {
            const content = editor.__quill.getText(selection.index, selection.length);
            clipboard.writeText(content).then(() => {
              editor.__quill.updateContents(
                new Delta().retain(selection.index).delete(selection.length), "user");
              editor.__quill.setSelection(selection.index, 0, "user");
            }).catch((e) => {
              console.log("Clipboard accessing error: " + e);
            });
          }
          this.chordProcessed = true;
          return;
        } else if (event.button === 2) {
          clipboard.readText().then(content => {
            content = content || "";
            const selection = editor.__quill.getSelection();
            editor.__quill.updateContents(
              new Delta().retain(selection.index)
                         .delete(selection.length)
                         .insert(content),
              "user");
            editor.__quill.setSelection(selection.index, content.length, "user");
          }).catch((e) => {
            console.log("Clipboard accessing error: " + e);
          });
          this.chordProcessed = true;
          return;
        }
      }
      this.leftButtonDown = event.button === 0;
      const action = this.extractAction(event);
      if (!action) { return; }
      this.mousedownSelection = editor.__quill.getSelection();
    });
    this.el.addEventListener("mouseup", event => {
      if (event.button === 0) {
        this.leftButtonDown = false;
      }
      if (this.chordProcessed) {
        this.chordProcessed = false;
        return;
      }
      const action = this.extractAction(event);
      if (!action) { return; }
      const editor = this.findEditor(event);
      if ((!editor) || (!editor.__quill)) { return; }
      // getSelection might refresh current selection and trigger onselection(),
      // however we want to keep original selection here, hence we are keeping
      // a copy of the original value here.
      const oldSelection = this.currentSelection;
      const selection = editor.__quill.getSelection();
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

      generate_action(action, editor, selection, api, oldSelection);
    });
    this.el.addEventListener("touchstart", event => {
      if (event.touches.length == 1) {
        const editor = this.findEditor(event);
        if ((!editor) || (!editor.__quill)) { return; }
        this.touchState = {
          editor,
          position: event.touches[0]
        };
      } else {
        if (!this.touchState) { return; }
        const { editor, position } = this.touchState;
        const oldSelection = this.currentSelection;
        const selection = editor.__quill.getSelection();
        if (!selection) { return; }
        const distance1 = Math.pow(position.pageX - event.touches[0].pageX, 2) +
                          Math.pow(position.pageY - event.touches[0].pageY, 2);
        const distance2 = Math.pow(position.pageX - event.touches[1].pageX, 2) +
                          Math.pow(position.pageY - event.touches[1].pageY, 2);
        const source = (distance1 < distance2) ? 0 : 1;
        const target = (distance1 < distance2) ? 1 : 0;
        const actionKey = (event.touches[source].pageY < event.touches[target].pageY) ? 1 : 2;
        const action = ACTIONS[actionKey];

        generate_action(action, editor, selection, api, oldSelection);
      }
    });
    if (!IS_MOBILE) {
      this.el.addEventListener("contextmenu", event => {
        event.preventDefault();
        return false;
      });
    }
    this.el.addEventListener("dragend", event => {
      const id = event.target.__id;
      if (!id) { return; }

      const { width, height } = windowDimension();
      const x = event.clientX / width * 100;
      const y = event.clientY / height * 100;
      api.move({id, x, y});
    });
  }

  updateEditorSizes() {
    const editors = document.querySelectorAll(".ql-container");
    const editorDimensions = {};
    let textWidth = 0;
    let textHeight = 0;
    for (const editor of editors) {
      const id = editor.__id;
      const rect = editor.getBoundingClientRect();
      editorDimensions[id] = {
        width: rect.width,
        height: rect.height
      };
      if (textWidth === 0 || textHeight === 0) {
        const quill = editor.__quill;
        if (quill.getText(0, 1) !== "\n") {
          const element = editor.querySelector(".ql-editor p");
          if (element) {
            const bounds = quill.getBounds(0, 1);
            const width = bounds.right - bounds.left;
            const height = parseFloat(getComputedStyle(element).lineHeight);
            if (width > 0 && height > 0) {
              textWidth = width;
              textHeight = height;
            }
          }
        }
      }
    }
    if (textWidth === 0 || textHeight === 0) {
      console.log("Unable to extract text metrics!");
      return;
    }
    const editorSizes = {};
    Object.keys(editorDimensions).forEach(id => {
      const { width, height } = editorDimensions[id];
      editorSizes[id] = {
        columns: Math.floor(width / textWidth) || 1,
        rows: Math.floor(height / textHeight) || 1
      };
    });
    this.api.sizechange(editorSizes);
  }

  extractAction(event) {
    let button = event.button;
    if (button === 0 && event.ctrlKey) {
      button = 1;
    }
    return ACTIONS[button];
  }

  findEditor(event) {
    let target = event.target;
    while (target != this.el && (!target.__quill) && target.parentElement) {
      target = target.parentElement;
    }
    return target;
  }

  update({layout, rows, dirtyChanges, selection}) {
    if (layout) {
      // Saving scroll positions for existing rows when layout needs changes.
      this.rows.views.forEach(row => {
        row.saveScroll();
      });
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

    if (rows) {
      const { lookup } = this.rows;
      rows.forEach(function(change) {
        const id = change.id - 1 + change.id % 2;
        const row = lookup[id];
        if (row) {
          row.update({ change });
        }
      });
    }

    if (dirtyChanges) {
      const { lookup } = this.rows;
      for (const [editorId, dirty] of Object.entries(dirtyChanges)) {
        const id = editorId - 1 + editorId % 2;
        const row = lookup[id];
        if (row) {
          row.update({ dirty });
        }
      }
    }

    if (layout) {
      // Restoring scroll positions
      this.rows.views.forEach(row => {
        row.restoreScroll();
      });
      this.updateEditorSizes();
    }

    if (selection) {
      const { lookup } = this.rows;
      const id = selection.id - 1 + selection.id % 2;
      const row = lookup[id];
      if (row) {
        row.update({ selection })
      }
    }
  }

  onselection(id, selection) {
    if (selection) {
      this.currentSelection = { id, range: selection };
    }
  }

  verify(hashes) {
    const { lookup } = this.rows;
    Object.keys(hashes).forEach(function(contentId) {
      contentId = parseInt(contentId);
      const id = contentId - 1 + contentId % 2;
      const row = lookup[id];
      if (row) {
        row.verify(contentId, hashes[contentId]);
      }
    });
  }
}

export class Row {
  constructor({ api, root }, { id }) {
    this.id = id;
    this.resizer = el(".resizer", {draggable: true});
    this.label = el(".label");
    this.header = el(".header", [this.resizer, this.label]);
    this.content = el(".content");
    this.el = el(".row", [this.header, this.content]);
    this.scrollPosition = null;

    this.labelEditor = new Quill(this.label);
    this.contentEditor = new Quill(this.content);

    this.resizer.__id = this.id;

    this.label.__id = id
    this.content.__id = id + 1;

    this.label.__version = 0;
    this.content.__version = 0;

    this.contentEditor.lastSelection = null;

    this.labelEditor.on("text-change", (delta, _oldDelta, source) => {
      if (source === "user") {
        api.textchange(this.label.__id, delta, this.label.__version);
      }
    });
    this.contentEditor.on("text-change", (delta, _oldDelta, source) => {
      if (source === "user") {
        api.textchange(this.content.__id, delta, this.content.__version);
      }
    });
    this.labelEditor.on("selection-change", (selection) => {
      root.onselection(this.label.__id, selection);
    });
    this.contentEditor.on("selection-change", (selection) => {
      root.onselection(this.content.__id, selection);
    });
  }

  update({height, change, dirty, selection}) {
    if (height) {
      setStyle(this.el, {height: `${height}%`});
    }
    if (change) {
      const id = change.id;
      const delta = change.delta;
      const version = change.version;
      if (id === this.label.__id) {
        if (delta) {
          this.labelEditor.updateContents(new Delta(delta));
        }
        if (version && version > this.label.__version) {
          this.label.__version = version;
        }
      } else if (id === this.content.__id) {
        if (delta) {
          this.contentEditor.updateContents(new Delta(delta));
        }
        if (version && version > this.content.__version) {
          this.content.__version = version;
        }
      }
    }
    if (typeof dirty === "boolean") {
      if (dirty) {
        this.resizer.classList.add("dirty");
      } else {
        this.resizer.classList.remove("dirty");
      }
    }
    if (selection) {
      this.contentEditor.setSelection(selection.range.index,
                                      selection.range.length);
    }
  }

  verify(id, hash) {
    if (id === this.label.__id) {
      verifyContent(this.labelEditor.getContents(), this.label.__version, hash);
    } else if (id === this.content.__id) {
      verifyContent(this.contentEditor.getContents(), this.content.__version, hash);
    } else {
      console.log("Unknown ID: " + id + " for row: " + this.id);
    }
  }

  saveScroll() {
    const scrollElement = this.content.querySelector(".ql-editor");
    const top = scrollElement ? Math.round(scrollElement.scrollTop) : 0;
    if (top > 0) {
      const count = scrollElement.children.length;
      for (let i = 0; i < count; i++) {
        const child = scrollElement.children[i];
        if ((i === count - 1) ||
            (Math.round(scrollElement.children[i + 1].offsetTop) > top)) {
          this.scrollPosition = child.offsetTop;
          return;
        }
      }
    }
    this.scrollPosition = null;
  }

  restoreScroll() {
    if (this.scrollPosition) {
      this.content.querySelector(".ql-editor").scrollTo(0, this.scrollPosition);
    }
  }
}

let nextColumnId = 1;
function columnId() {
  return nextColumnId++;
}

export class Layout {
  constructor() {
    this.version = 0;
    this.data = new Delta();
    this.sizes = {};
    this.dirty = false;
    this.columns = [
      {
        id: columnId(),
        width: 50,
        rows: []
      },
      {
        id: columnId(),
        width: 50,
        rows: []
      }
    ];
  }

  createColumn() {
    const column = {
      id: columnId(),
      width: this.columns[this.columns.length - 1].width / 2,
      rows: []
    };
    this.columns[this.columns.length - 1].width -= column.width;
    this.columns.push(column);
  }

  removeColumn(rowId) {
    if (this.columns.length === 1) {
      return;
    }
    const columnIndex = this.columns.findIndex(column => {
      return column.rows.findIndex(row => row.id === rowId) !== -1;
    });
    if (columnIndex !== -1) {
      const column = this.columns[columnIndex];
      this.columns.splice(columnIndex, 1);
      this.columns[this.columns.length - 1].width += column.width;
      column.rows.forEach(row => {
        this._createRow(row.id);
      });
    }
  }

  verify(hash) {
    verifyContent(this.data, this.version, hash);
  }

  update(change) {
    this.version = Math.max(this.version, change.version);
    this.data = this.data.compose(new Delta(change.delta));
    const currentIds = this._currentIds();
    const oldIds = [].concat(...this.columns.map(column => column.rows.map(row => row.id)));
    const addedIds = currentIds.filter(id => !oldIds.includes(id));
    const deletedIds = oldIds.filter(id => !currentIds.includes(id));
    deletedIds.forEach(id => {
      this._deleteRow(id);
      delete this.sizes[id];
      delete this.sizes[id + 1];
    });
    addedIds.forEach(id => {
      this._createRow(id);
      this.sizes[id] = { columns: 0, rows: 0 };
      this.sizes[id + 1] = { columns: 0, rows: 0 };
    });
    return addedIds.length > 0 || deletedIds.length > 0;
  }

  updateSizes(sizes) {
    Object.keys(sizes).forEach(id => {
      const { columns, rows } = sizes[id];
      if (this.sizes[id] &&
          (this.sizes[id].columns !== columns ||
           this.sizes[id].rows !== rows)) {
        this.sizes[id].columns = columns;
        this.sizes[id].rows = rows;
        this.dirty = true;
      }
    });
  }

  move(id, x, y) {
    const source = this._locateEditorById(id);
    const target = this._locateEditorByPosition(x, y);

    if (!(source && target)) {
      return;
    }

    if (source.row === 0 && target.row === 0 &&
        source.column === target.column &&
        target.position < 5) {
      /* Shrinking column */
      if (target.column > 0) {
        this.columns[target.column - 1].width += target.xPosition;
        this.columns[target.column].width -= target.xPosition;
      }
    } else if (source.row === 0 && target.row === 0 &&
               source.column === target.column + 1 &&
               target.position < 5) {
      /* Enlarging column */
      const diff = this.columns[target.column].width - target.xPosition;
      this.columns[target.column].width -= diff;
      this.columns[source.column].width += diff;
    } else if (source.column === target.column &&
        source.row === target.row) {
      /* Shrinking row */
      if (target.row > 0) {
        this.columns[target.column].rows[target.row - 1].height += target.position;
        this.columns[target.column].rows[target.row].height -= target.position;
      }
    } else if (source.column === target.column &&
               source.row === target.row + 1) {
      /* Enlarging row */
      const diff = this.columns[target.column].rows[target.row].height - target.position;
      this.columns[target.column].rows[target.row].height -= diff;
      this.columns[source.column].rows[source.row].height += diff;
    } else if (target.row === -1) {
      /* Moving row to an empty column */
      this._deleteRow(id);
      this.columns[target.column].rows.splice(0, 0, {
        height: 100,
        id,
      });
    } else {
      /* Moving row to a new location */
      const targetId = this.columns[target.column].rows[target.row].id;
      this._deleteRow(id);
      const newTargetRow = this.columns[target.column].rows.findIndex(({id}) => id === targetId);
      if (newTargetRow === -1) {
        console.log("Unexpected!");
        return;
      }
      const remaining = this.columns[target.column].rows[newTargetRow].height - target.position;
      this.columns[target.column].rows[newTargetRow].height = target.position;
      this.columns[target.column].rows.splice(newTargetRow + 1, 0, {
        height: remaining,
        id,
      });
    }
  }

  knownIds() {
    return this.data.filter(op => typeof op.insert === "string")
                           .map(op => op.insert)
                           .join("")
                           .split("\n")
                           .map(line => parseInt(line, 10));
  }

  _currentIds() {
    return this.knownIds().filter(id => id > 0 && id % 2 !== 0);
  }

  _locateEditorById(id) {
    for (const [columnIndex, column] of this.columns.entries()) {
      for (const [rowIndex, row] of column.rows.entries()) {
        if (row.id === id) {
          return {
            column: columnIndex,
            row: rowIndex,
          };
        }
      }
    }
    return null;
  }

  _locateEditorByPosition(x, y) {
    let targetColumn = -1;
    let currentWidth = 0;
    for (const [columnIndex, column] of this.columns.entries()) {
      if (x < currentWidth + column.width) {
        targetColumn = columnIndex;
        break;
      }
      currentWidth += column.width;
    }
    if (targetColumn === -1) {
      console.error("Cannot locate dropped column!");
      return null;
    }

    let targetRow = -1;
    let currentHeight = 0;
    for (const [rowIndex, row] of this.columns[targetColumn].rows.entries()) {
      if (y < currentHeight + row.height) {
        targetRow = rowIndex;
        break;
      }
      currentHeight += row.height;
    }

    return {
      column: targetColumn,
      row: targetRow,
      position: y - currentHeight,
      xPosition: x - currentWidth,
    };
  }

  _createRow(rowId) {
    let columnIndex = -1;
    let columnSpareHeight = 0;

    this.columns.forEach(function(column, idx) {
      let currentHeight = 100;
      if (column.rows.length > 0) {
        currentHeight = column.rows[column.rows.length - 1].height / 2;
      }
      if (currentHeight > columnSpareHeight) {
        columnIndex = idx;
        columnSpareHeight = currentHeight;
      }
    });

    if (columnIndex === -1) {
      console.error("Cannot find column to insert!");
      return;
    }

    const column = this.columns[columnIndex];
    if (column.rows.length > 0) {
      column.rows[column.rows.length - 1].height -= columnSpareHeight;
    }
    column.rows.push({
      height: columnSpareHeight,
      id: rowId
    });
    return rowId;
  }

  _deleteRow(rowId) {
    this.columns.forEach(function(column) {
      const index = column.rows.findIndex(function(row) {
        return row.id === rowId;
      });
      if (index !== -1) {
        const growIndex = index === 0 ? index + 1 : index - 1;
        if (growIndex < column.rows.length) {
          column.rows[growIndex].height += column.rows[index].height;
        }
        column.rows.splice(index, 1);
      }
    });
  }
}
