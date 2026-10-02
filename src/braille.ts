// A braille cell is 2x4 dots. DOT_BITS[y][x] is the bit for the dot at (x, y) within a cell.
const DOT_BITS = [
  [0x01, 0x08],
  [0x02, 0x10],
  [0x04, 0x20],
  [0x40, 0x80],
];

export interface Cell {
  char: string;
  series: number; // index of the series drawn last in this cell, -1 if empty
}

// Plots line charts into a cols x rows grid of braille cells (cols*2 x rows*4 dots).
// Each series holds one value per dot column; null is a gap and breaks the line.
export function plotBraille(
  series: Array<Array<number | null>>,
  cols: number,
  rows: number,
  min: number,
  max: number,
): Cell[][] {
  const height = rows * 4;
  const bits = Array.from({ length: rows }, () => new Array<number>(cols).fill(0));
  const owner = Array.from({ length: rows }, () => new Array<number>(cols).fill(-1));
  const toY = (v: number) =>
    Math.min(height - 1, Math.max(0, Math.round(((max - v) / (max - min)) * (height - 1))));

  series.forEach((values, s) => {
    let prev: number | null = null;
    values.slice(0, cols * 2).forEach((v, x) => {
      if (v === null) {
        prev = null;
        return;
      }
      const y = toY(v);
      // Fill the vertical span from the previous point so steep changes stay connected
      const from = prev === null ? y : Math.min(prev, y);
      const to = prev === null ? y : Math.max(prev, y);
      for (let yy = from; yy <= to; yy++) {
        bits[yy >> 2][x >> 1] |= DOT_BITS[yy & 3][x & 1];
        owner[yy >> 2][x >> 1] = s;
      }
      prev = y;
    });
  });

  return bits.map((row, r) =>
    row.map((b, c) => ({
      char: b ? String.fromCharCode(0x2800 + b) : ' ',
      series: owner[r][c],
    })),
  );
}
