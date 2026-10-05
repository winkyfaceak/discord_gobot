package commands

import "strings"

const c4Rows, c4Cols = 6, 7

var (
	c4Pieces  = [3]string{"⚪", "🔴", "🟡"} // empty, then each player's colour
	c4Numbers = [c4Cols]string{"1️⃣", "2️⃣", "3️⃣", "4️⃣", "5️⃣", "6️⃣", "7️⃣"}
)

// c4Board is a Connect 4 grid, row 0 at the top. Cells are 0 when empty,
// else the piece's index into c4Pieces.
type c4Board [c4Rows][c4Cols]int

// drop lets piece fall into col and returns the row it lands on, or -1 if
// the column is full.
func (b *c4Board) drop(col, piece int) int {
	for row := c4Rows - 1; row >= 0; row-- {
		if b[row][col] == 0 {
			b[row][col] = piece
			return row
		}
	}
	return -1
}

// wins reports whether the piece at row, col is part of four in a row.
func (b *c4Board) wins(row, col int) bool {
	piece := b[row][col]
	for _, dir := range [][2]int{{0, 1}, {1, 0}, {1, 1}, {1, -1}} {
		inLine := 1
		for _, sign := range []int{1, -1} {
			dr, dc := sign*dir[0], sign*dir[1]
			for r, c := row+dr, col+dc; r >= 0 && r < c4Rows && c >= 0 && c < c4Cols && b[r][c] == piece; r, c = r+dr, c+dc {
				inLine++
			}
		}
		if inLine >= 4 {
			return true
		}
	}
	return false
}

// full reports whether no column has room.
func (b *c4Board) full() bool {
	for _, cell := range b[0] {
		if cell == 0 {
			return false
		}
	}
	return true
}

func (b *c4Board) String() string {
	var s strings.Builder
	s.WriteString(strings.Join(c4Numbers[:], "") + "\n")
	for _, row := range b {
		for _, cell := range row {
			s.WriteString(c4Pieces[cell])
		}
		s.WriteString("\n")
	}
	return s.String()
}
