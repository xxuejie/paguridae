import { document, Quill } from "./externals.js";
const Delta = Quill.import("delta");

const LAYOUT_ID = 0;

let nextColumnId = 1;

export function columnId() {
  return nextColumnId++;
}

class Layout {
  constructor() {
    this.data = new Delta();
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

  update(changes) {
    this.data = changes.reduce((data, change) => data.compose(new Delta(change.change)), this.data);
    const currentIds = this.data.filter(op => typeof op.insert === "string")
                           .map(op => op.insert)
                           .join("")
                           .split("\n")
                           .map(line => parseInt(line, 10))
                           .filter(id => id > 0 && id % 2 !== 0);
    const oldIds = [].concat(...this.columns.map(column => column.rows.map(row => row.id)));
    const addedIds = currentIds.filter(id => !oldIds.includes(id));
    const deletedIds = oldIds.filter(id => !currentIds.includes(id));
    deletedIds.forEach(id => this._deleteRow(id));
    addedIds.forEach(id => this._createRow(id));
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
      window.alert("Not connected!");
      return;
    }
    /* All communication is asynchronous, no need to get a response here */
    this.ws.send(JSON.stringify(message));
  }
}

export class Api {
  constructor() {
    this.layout = new Layout();
    this.rows = {};
  }

  init(onchange) {
    this.onchange = onchange;
    this.connection = new Connection(changes => {
      const layoutChanges = changes.filter(change => change.id === LAYOUT_ID);
      const editorChanges = {
        rows: changes.filter(change => change.id != LAYOUT_ID).map(({id, change}) => {
          if (id % 2 === 0) {
            return {
              id: id - 1,
              content: change
            };
          } else {
            return {
              id,
              label: change
            };
          }
        })
      };
      if (layoutChanges.length > 0) {
        this.layout.update(layoutChanges);
        editorChanges["layout"] = {
          columns: this.layout.columns
        };
      }
      this.onchange(editorChanges);
    });
    this.connection.connect();
  }

  textchange(id, delta) {
    this.rows[id] = this.rows[id] || { id: id };
    this.rows[id].change = this.rows[id].change || new Delta();
    this.rows[id].change = this.rows[id].change.compose(delta);
  }

  action(data) {
    const rows = Object.values(this.rows);
    this.rows = {};
    this.connection.send({
      rows,
      action: data
    });
  }

  move({id, x, y}) {
    console.log(`Moving ${id}, x: ${x}, y: ${y}`);
  }
}
