let nextId = 1;

export function id() {
  return nextId++;
}

class Layout {
  constructor() {
    this.data = {
      columns: [
        {
          id: id(),
          width: 50,
          rows: []
        },
        {
          id: id(),
          width: 50,
          rows: []
        }
      ]
    };
  }

  createRow() {
    const rowId = id();
    let columnIndex = -1;
    let columnSpareHeight = 0;

    this.data.columns.forEach(function(column, idx) {
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

    const column = this.data.columns[columnIndex];
    if (column.rows.length > 0) {
      column.rows[column.rows.length - 1].height -= columnSpareHeight;
    }
    column.rows.push({
      height: columnSpareHeight,
      id: rowId
    });
    return rowId;
  }

  deleteRow(rowId) {
    this.data.columns.forEach(function(column) {
      const index = column.rows.findIndex(function(row) {
        return row.id === rowId;
      });
      if (index !== -1) {
        column.rows.splice(index, 1);
      }
    });
  }
}

export class Api {
  constructor(onchange) {
    this.onchange = onchange;
    this.layout = new Layout();

    const row1Id = this.layout.createRow();
    const row2Id = this.layout.createRow();
    this.layout.createRow();

    onchange({
      layout: this.layout.data,
      changes: [
        {
          id: row1Id,
          content: [
            {
              insert: "Foobar\nLine 2\n\nAnotherLine"
            }
          ]
        },
        {
          id: row2Id,
          label: [
            {
              insert: "~ | New Newcol Cut Copy Paste"
            }
          ]
        }
      ]
    });
  }
}
