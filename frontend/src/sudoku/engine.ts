export type SudokuSize = 4 | 9
export type SudokuDifficulty = 'easy' | 'normal' | 'hard' | 'expert'

export interface SudokuPuzzle {
  size: SudokuSize
  boxSize: number
  puzzle: number[]
  solution: number[]
}

const CLUE_TARGETS: Record<SudokuSize, Record<SudokuDifficulty, number>> = {
  4: { easy: 12, normal: 10, hard: 8, expert: 6 },
  9: { easy: 40, normal: 34, hard: 28, expert: 24 },
}

const randInt = (maxExclusive: number): number => {
  if (maxExclusive <= 0) return 0
  const cryptoObj = globalThis.crypto
  if (cryptoObj?.getRandomValues) {
    const x = new Uint32Array(1)
    cryptoObj.getRandomValues(x)
    return x[0] % maxExclusive
  }
  return Math.floor(Math.random() * maxExclusive)
}

const shuffleInPlace = <T>(arr: T[]): T[] => {
  for (let i = arr.length - 1; i > 0; i--) {
    const j = randInt(i + 1)
    const tmp = arr[i]
    arr[i] = arr[j]
    arr[j] = tmp
  }
  return arr
}

const popcount = (x: number): number => {
  let c = 0
  let v = x >>> 0
  while (v) {
    v &= v - 1
    c++
  }
  return c
}

const maskToValues = (mask: number, size: number): number[] => {
  const out: number[] = []
  for (let v = 1; v <= size; v++) {
    if (mask & (1 << v)) out.push(v)
  }
  return out
}

type Masks = {
  row: number[]
  col: number[]
  box: number[]
}

const initMasks = (board: number[], size: number): Masks | null => {
  const boxSize = Math.sqrt(size)
  if (!Number.isInteger(boxSize)) {
    throw new Error(`invalid size ${size}`)
  }
  const row = new Array<number>(size).fill(0)
  const col = new Array<number>(size).fill(0)
  const box = new Array<number>(size).fill(0)
  for (let i = 0; i < board.length; i++) {
    const v = board[i] | 0
    if (!v) continue
    if (v < 1 || v > size) return null
    const bit = 1 << v
    const r = Math.floor(i / size)
    const c = i % size
    const b = Math.floor(r / boxSize) * boxSize + Math.floor(c / boxSize)
    if (row[r] & bit) return null
    if (col[c] & bit) return null
    if (box[b] & bit) return null
    row[r] |= bit
    col[c] |= bit
    box[b] |= bit
  }
  return { row, col, box }
}

const candidateMask = (m: Masks, size: number, boxSize: number, idx: number): number => {
  const full = ((1 << (size + 1)) - 1) & ~1
  const r = Math.floor(idx / size)
  const c = idx % size
  const b = Math.floor(r / boxSize) * boxSize + Math.floor(c / boxSize)
  return full & ~(m.row[r] | m.col[c] | m.box[b])
}

const place = (board: number[], m: Masks, size: number, boxSize: number, idx: number, v: number) => {
  board[idx] = v
  const bit = 1 << v
  const r = Math.floor(idx / size)
  const c = idx % size
  const b = Math.floor(r / boxSize) * boxSize + Math.floor(c / boxSize)
  m.row[r] |= bit
  m.col[c] |= bit
  m.box[b] |= bit
}

const unplace = (board: number[], m: Masks, size: number, boxSize: number, idx: number, v: number) => {
  board[idx] = 0
  const bit = ~(1 << v)
  const r = Math.floor(idx / size)
  const c = idx % size
  const b = Math.floor(r / boxSize) * boxSize + Math.floor(c / boxSize)
  m.row[r] &= bit
  m.col[c] &= bit
  m.box[b] &= bit
}

const solveOne = (board: number[], size: number, randomize: boolean): number[] | null => {
  const boxSize = Math.sqrt(size)
  const masks = initMasks(board, size)
  if (!masks) return null

  const search = (): boolean => {
    let bestIdx = -1
    let bestMask = 0
    let bestCount = 999
    for (let i = 0; i < board.length; i++) {
      if (board[i] !== 0) continue
      const mask = candidateMask(masks, size, boxSize, i)
      const count = popcount(mask)
      if (count === 0) return false
      if (count < bestCount) {
        bestCount = count
        bestIdx = i
        bestMask = mask
        if (count === 1) break
      }
    }
    if (bestIdx === -1) return true

    const values = maskToValues(bestMask, size)
    if (randomize) shuffleInPlace(values)
    for (const v of values) {
      place(board, masks, size, boxSize, bestIdx, v)
      if (search()) return true
      unplace(board, masks, size, boxSize, bestIdx, v)
    }
    return false
  }

  return search() ? board.slice() : null
}

const countSolutions = (board: number[], size: number, limit: number): number => {
  const boxSize = Math.sqrt(size)
  const working = board.slice()
  const masks = initMasks(working, size)
  if (!masks) return 0
  let found = 0

  const search = () => {
    if (found >= limit) return

    let bestIdx = -1
    let bestMask = 0
    let bestCount = 999
    for (let i = 0; i < working.length; i++) {
      if (working[i] !== 0) continue
      const mask = candidateMask(masks, size, boxSize, i)
      const count = popcount(mask)
      if (count === 0) return
      if (count < bestCount) {
        bestCount = count
        bestIdx = i
        bestMask = mask
        if (count === 1) break
      }
    }
    if (bestIdx === -1) {
      found++
      return
    }

    for (let v = 1; v <= size; v++) {
      if (!(bestMask & (1 << v))) continue
      place(working, masks, size, boxSize, bestIdx, v)
      search()
      unplace(working, masks, size, boxSize, bestIdx, v)
      if (found >= limit) return
    }
  }

  search()
  return found
}

export const generateSudokuPuzzle = (size: SudokuSize, difficulty: SudokuDifficulty): SudokuPuzzle => {
  const boxSize = Math.sqrt(size)
  if (!Number.isInteger(boxSize)) {
    throw new Error(`invalid size ${size}`)
  }
  const clueTarget = CLUE_TARGETS[size][difficulty]
  const removalTarget = size * size - clueTarget

  let best: { puzzle: number[]; solution: number[]; removed: number } | null = null
  const maxAttempts = 6

  for (let attempt = 0; attempt < maxAttempts; attempt++) {
    const empty = new Array<number>(size * size).fill(0)
    const solved = solveOne(empty, size, true)
    if (!solved) continue

    const puzzle = solved.slice()
    let removed = 0
    const indices = shuffleInPlace(Array.from({ length: puzzle.length }, (_, i) => i))

    for (const idx of indices) {
      if (removed >= removalTarget) break
      const prev = puzzle[idx]
      if (!prev) continue
      puzzle[idx] = 0
      const solutions = countSolutions(puzzle, size, 2)
      if (solutions === 1) {
        removed++
      } else {
        puzzle[idx] = prev
      }
    }

    if (!best || removed > best.removed) {
      best = { puzzle: puzzle.slice(), solution: solved.slice(), removed }
    }
    if (removed >= removalTarget) {
      return { size, boxSize, puzzle, solution: solved }
    }
  }

  if (!best) {
    throw new Error('failed to generate puzzle')
  }
  return { size, boxSize, puzzle: best.puzzle, solution: best.solution }
}

