let nextId = 1;

function id() {
  return nextId++;
}

function init() {
  return {
    columns: [
      {
        id: id(),
        rows: [
          {
            id: id(),
            content: [
              {
                insert: "Foobar\nLine 2\n\nAnotherLine"
              }
            ]
          }
        ]
      },
      {
        id: id(),
        rows: [
          {
            id: id(),
            label: [
              {
                insert: "~ | New Newcol Cut Copy Paste"
              }
            ]
          },
          {
            id: id()
          }
        ]
      }
    ]
  }
}

export class Api {
  constructor(onchange) {
    this.onchange = onchange;
    this.data = init();
    onchange(this.data);
  }
}
