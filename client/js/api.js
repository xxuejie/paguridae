let nextId = 1;

function id() {
  return nextId++;
}

export function init() {
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
