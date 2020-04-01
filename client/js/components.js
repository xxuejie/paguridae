import { clipboard, document, redom, verifyContent, Quill } from "./externals.js";

const {el, listPool, setChildren, setStyle} = redom;
const Delta = Quill.import("delta");

const ACTIONS = {
  // Middle click
  1: "execute",
  // Right click
  2: "search"
};
Object.freeze(ACTIONS);

const IS_MOBILE = /Android|iPhone|iPad/i.test(window.navigator.userAgent);

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

    window.addEventListener("resize", () => {
      this.updateEditorSizes();
    });

    api.init(data => {
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

      const x = event.clientX / window.innerWidth * 100;
      const y = event.clientY / window.innerHeight * 100;
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
            const height = parseFloat(window.getComputedStyle(element).lineHeight);
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

  update({layout, rows}) {
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

    if (layout) {
      // Restoring scroll positions
      this.rows.views.forEach(row => {
        row.restoreScroll();
      });
      this.updateEditorSizes();
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

  update({height, change}) {
    if (height) {
      setStyle(this.el, {height: `${height}%`});
    }
    if (change) {
      const id = change.id;
      const delta = change.change && change.change.delta;
      const version = change.change && change.change.version;
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
  }

  verify(id, hash) {
    if (id === this.label.__id) {
      verifyContent(this.labelEditor.getContents(), hash);
    } else if (id === this.content.__id) {
      verifyContent(this.contentEditor.getContents(), hash);
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
