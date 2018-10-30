export function fill(items, key) {
  const count = items.length;
  if (count <= 0) { return items; }
  const space = 100 / count;
  const lastSpace = 100 - (space * (count - 1));

  return items.map(function(item, idx) {
    const currentSpace = (idx == count - 1) ? lastSpace : space;
    return Object.assign({}, item, {
      [key]: currentSpace
    });
  });
}
