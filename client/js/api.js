import { document, Quill } from "./externals.js";
const Delta = Quill.import("delta");

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

  onchange(change) {
    this.data = this.data.compose(new Delta(change));
    const currentIds = this.data.filter(op => typeof op.insert === "string")
                           .map(op => op.insert)
                           .join("")
                           .split("\n")
                           .map(line => parseInt(line, 10));
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
      console.log(`Connection closed, reconnect in ${self.backoff} milliseconds`);
      window.setTimeout(function () {
        self.connect();
      }, self.backoff);
      self.backoff = self.backoff * 2;
    };
    ws.onmessage = function (event) {
      self.onchange(JSON.parse(event.data));
    }
    ws.onopen = function (event) {
      console.log("Connection initiated!");
      self.backoff = INITIAL_BACKOFF_MS;
    }
  }

  send(message) {
    /* All communication is asynchronous, no need to get a response here */
    this.ws.send(JSON.stringify(message));
  }
}

export class Api {
  constructor() {
    this.layout = new Layout();
  }

  init(onchange) {
    this.onchange = onchange;
    this.connection = new Connection(data => {
      this.layout.onchange(data.layout);
      this.onchange({
        layout: {
          columns: this.layout.columns,
        },
        rows: data.rows
      });
    });
    this.connection.connect();
  }

  execute({id, type, selection}) {
    const {index, length} = selection;
    console.log(`Executing on ${id}, type: ${type}, index: ${index}, length: ${length}`);
  }

  search({id, type, selection}) {
    const {index, length} = selection;
    console.log(`Searching on ${id}, type: ${type}, index: ${index}, length: ${length}`);
  }

  move({id, x, y}) {
    console.log(`Moving ${id}, x: ${x}, y: ${y}`);
  }
}
