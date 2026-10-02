import { expect, test } from 'bun:test';
import { plotBraille } from './braille';

const chars = (series: Array<Array<number | null>>, cols: number, rows: number) =>
  plotBraille(series, cols, rows, 0, 1).map((row) => row.map((c) => c.char).join(''));

test('flat line at min sits on the bottom dot row', () => {
  expect(chars([[0, 0, 0, 0]], 2, 2)).toEqual(['  ', '⣀⣀']);
});

test('rise is connected vertically', () => {
  // x0 at bottom, x1 jumps to top: right column fully filled
  expect(chars([[0, 1]], 1, 1)).toEqual(['⣸']);
});

test('null breaks the line', () => {
  expect(chars([[0, null, 1, 1]], 2, 1)).toEqual(['⡀⠉']);
});

test('cell remembers the last series drawn', () => {
  const cells = plotBraille([[0, 0], [1, 1]], 1, 1, 0, 1);
  expect(cells[0][0]).toEqual({ char: '⣉', series: 1 });
});
