import { document, signalError, verifyContent, Quill } from "./externals.js";
const Delta = Quill.import("delta");

const LAYOUT_ID = 0;

let nextColumnId = 1;

export function columnId() {
  return nextColumnId++;
}

class Layout {
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

  verify(hash) {
    verifyContent(this.data, hash);
  }

  update(changes) {
    changes.forEach(change => {
      this.version = Math.max(this.version, change.change.version);
    });
    this.data = changes.reduce((data, change) => data.compose(new Delta(change.change.delta)), this.data);
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

  generateSizeChange() {
    if (!this.dirty) {
      return null;
    }
    let d = new Delta();
    this.data.eachLine(line => {
      const text = line.map(op => {
        return (typeof op.insert === "string") ? op.insert : "";
      }).join("");
      const space = text.indexOf(" ");
      if (space !== -1) {
        const id = parseInt(text.substring(0, space));
        if (this.sizes[id]) {
          d = d.retain(space).delete(text.length - space)
               .insert(` ${this.sizes[id].columns} ${this.sizes[id].rows}`).retain(1);
          return;
        }
      }
      d = d.retain(text.length + 1);
    });
    this.data = this.data.compose(d);
    this.dirty = false;
    return d;
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

  _currentIds() {
    return this.data.filter(op => typeof op.insert === "string")
                           .map(op => op.insert)
                           .join("")
                           .split("\n")
                           .map(line => parseInt(line, 10))
                           .filter(id => id > 0 && id % 2 !== 0);
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

const INITIAL_BACKOFF_MS = 1000;
class Connection {
  constructor(onchange) {
    this.onchange = onchange;
  }

  connect() {
    const self = this;
    const ws = this.ws = new window.WebSocket("ws://" + document.location.host + "/ws");
    ws.onclose = ws.onerror = function() {
      /* TODO: Add reconnect logic once we have state syncing in place. */
      console.log("Disconnected!");
      self.connected = false;
      /* console.log(`Connection closed, reconnect in ${self.backoff} milliseconds`);
       * window.setTimeout(function () {
       *   self.connect();
       * }, self.backoff);
       * self.backoff = self.backoff * 2; */
    };
    ws.onmessage = function (event) {
      self.onchange(JSON.parse(event.data));
    }
    ws.onopen = function (event) {
      console.log("Connection initiated!");
      self.connected = true;
      self.backoff = INITIAL_BACKOFF_MS;
    }
  }

  send(message) {
    if (!this.connected) {
      signalError("Not connected!");
      return;
    }
    /* All communication is asynchronous, no need to get a response here */
    this.ws.send(JSON.stringify(message));
  }
}

export class Api {
  constructor() {
    this.layout = new Layout();
    this.buffered_changes = {};
  }

  dirtyContentIds() {
    return Object.keys(this.buffered_changes).filter(id => id !== LAYOUT_ID && id % 2 === 0);
  }

  init(onchange, onverify) {
    this.onchange = onchange;
    this.onverify = onverify;
    this.connection = new Connection(({changes, hashes}) => {
      changes = changes || [];
      const editorChanges = {};
      if (Object.keys(this.buffered_changes).length !== 0) {
        changes = changes.map(change => {
          const localDelta = this.buffered_changes[change.id];
          const remoteDelta = change.change.delta;
          if (localDelta && remoteDelta) {
            // TODO: verify the logic here later
            const ld = localDelta.change.delta;
            const rd = new Delta(change.change.delta);
            this.buffered_changes[change.id].change.delta = rd.transform(ld, true);
            this.buffered_changes[change.id].change.version = change.change.version;
            change.change.delta = ld.transform(rd, false);
          }
          return change;
        });
        editorChanges.dirtyContentIds = this.dirtyContentIds();
      }
      editorChanges.rows = changes.filter(change => change.id != LAYOUT_ID);
      const layoutChanges = changes.filter(change => change.id === LAYOUT_ID);
      if (layoutChanges.length > 0) {
        this.layout.update(layoutChanges);
        editorChanges["layout"] = {
          columns: this.layout.columns
        };
      }
      this.onchange(editorChanges);
      if (hashes) {
        if (hashes[LAYOUT_ID]) {
          this.layout.verify(hashes[LAYOUT_ID]);
          delete hashes[LAYOUT_ID];
        }
        this.onverify(hashes);
      }
    });
    this.connection.connect();
  }

  textchange(id, delta, version) {
    let changed = !this.buffered_changes[id];
    this.buffered_changes[id] = this.buffered_changes[id] || { id: id, change: { version: version } };
    if (this.buffered_changes[id].change.version !== version) {
      signalError("Version mismatch, something is wrong!");
      return;
    }
    const aggregated = (this.buffered_changes[id].change.delta || new Delta()).compose(delta);
    if (aggregated.ops.length === 0) {
      changed = true;
      delete this.buffered_changes[id];
    } else {
      this.buffered_changes[id].change.delta = aggregated;
    }
    if (changed) {
      this.onchange({
        dirtyContentIds: this.dirtyContentIds()
      });
    }
  }

  action(data) {
    const d = this.layout.generateSizeChange();
    if (d) {
      this.textchange(LAYOUT_ID, d, this.layout.version);
    }
    const payload = {
      action: data,
      changes: Object.values(this.buffered_changes)
    };
    // TODO: do we need to wait for ack?
    this.buffered_changes = {};
    this.onchange({
      dirtyContentIds: this.dirtyContentIds()
    });
    this.connection.send(payload);
  }

  move({id, x, y}) {
    this.layout.move(id, x, y);
    this.onchange({ layout: { columns: this.layout.columns } });
  }

  sizechange(sizes) {
    this.layout.updateSizes(sizes);
  }
}
