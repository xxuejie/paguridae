import { redom, Quill } from "./externals.js";

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
function generate_action(action, editor, { index, length }, api) {
  const start = (index > ACTION_SELECTION_EXPAND_LENGTH) ?
                (index - ACTION_SELECTION_EXPAND_LENGTH) :
                0;
  const firstHalf = editor.__quill.getText(start, index - start);
  const secondHalf = editor.__quill.getText(index, ACTION_SELECTION_EXPAND_LENGTH);
  const firstHalfMatch = (firstHalf.match(/\S+$/) || [""])[0];
  const secondHalfMatch = (secondHalf.match(/^\S+/) || [""])[0];
  const selection = `${firstHalfMatch}${secondHalfMatch}`;
  api.action({
    action,
    id: editor.__id,
    index,
    selection
  });
}

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
      if ((!editor) || (!editor.__quill)) { return; }
      this.mousedownSelection = editor.__quill.getSelection();
    });
    this.el.addEventListener("mouseup", event => {
      const action = this.extractAction(event);
      if (!action) { return ;}
      const editor = this.findEditor(event);
      if ((!editor) || (!editor.__quill)) { return; }
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

      generate_action(action, editor, selection, api);
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

        generate_action(action, editor, selection, api);
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
  }
}

export class Row {
  constructor(api, { id }) {
    this.id = id;
    this.resizer = el(".resizer", {draggable: true});
    this.label = el(".label");
    this.header = el(".header", [this.resizer, this.label]);
    this.content = el(".content");
    this.el = el(".row", [this.header, this.content]);

    this.labelEditor = new Quill(this.label);
    this.contentEditor = new Quill(this.content);

    this.resizer.__id = this.id;

    this.label.__id = id
    this.content.__id = id + 1;

    this.label.__version = 0;
    this.content.__version = 0;

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
  }

  update({height, change}) {
    if (height) {
      setStyle(this.el, {height: `${height}%`});
    }
    if (change) {
      const {id, delta, version} = change;
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
}
