<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { generateSudokuPuzzle, type SudokuDifficulty, type SudokuSize } from '../sudoku/engine'
import { Check, Eraser, Lightbulb, Pencil, Redo2, RefreshCw, RotateCcw, Undo2 } from 'lucide-vue-next'
import { useI18n } from '../i18n'

type Action = {
  idx: number
  prevValue: number
  nextValue: number
  prevNotes: number
  nextNotes: number
}

const { t } = useI18n()

const size = ref<SudokuSize>(9)
const difficulty = ref<SudokuDifficulty>('normal')
const boxSize = computed(() => Math.sqrt(size.value))
const digits = computed(() => Array.from({ length: size.value }, (_, i) => i + 1))

const generating = ref(false)
const generationError = ref('')

const puzzle = ref<number[]>([])
const solution = ref<number[]>([])
const cells = ref<number[]>([])
const fixed = ref<boolean[]>([])
const notes = ref<number[]>([])

const selected = ref<number | null>(null)
const notesMode = ref(false)
const hintsUsed = ref(0)

const history = ref<Action[]>([])
const historyPtr = ref(0)

const startedAt = ref(0)
const solvedAt = ref(0)
const clockNow = ref(Date.now())
let clockTimer: number | null = null

const elapsedMs = computed(() => {
  if (!startedAt.value) return 0
  const end = solvedAt.value || clockNow.value
  return Math.max(0, end - startedAt.value)
})

const formatElapsed = (ms: number): string => {
  const total = Math.floor(ms / 1000)
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m}:${String(s).padStart(2, '0')}`
}

const elapsedText = computed(() => formatElapsed(elapsedMs.value))

const conflictMap = computed(() => {
  const n = size.value
  const out = new Array<boolean>(n * n).fill(false)

  const markDup = (indices: number[]) => {
    const seen = new Map<number, number>()
    for (const idx of indices) {
      const v = cells.value[idx] | 0
      if (!v) continue
      const first = seen.get(v)
      if (first === undefined) {
        seen.set(v, idx)
        continue
      }
      out[idx] = true
      out[first] = true
    }
  }

  for (let r = 0; r < n; r++) {
    markDup(Array.from({ length: n }, (_, c) => r * n + c))
  }
  for (let c = 0; c < n; c++) {
    markDup(Array.from({ length: n }, (_, r) => r * n + c))
  }

  const bs = boxSize.value
  for (let br = 0; br < bs; br++) {
    for (let bc = 0; bc < bs; bc++) {
      const indices: number[] = []
      for (let r = 0; r < bs; r++) {
        for (let c = 0; c < bs; c++) {
          const rr = br * bs + r
          const cc = bc * bs + c
          indices.push(rr * n + cc)
        }
      }
      markDup(indices)
    }
  }

  return out
})

const isSolved = computed(() => {
  if (!solution.value.length) return false
  for (let i = 0; i < cells.value.length; i++) {
    if ((cells.value[i] | 0) !== (solution.value[i] | 0)) return false
  }
  return true
})

const canUndo = computed(() => historyPtr.value > 0)
const canRedo = computed(() => historyPtr.value < history.value.length)

const pushAction = (action: Action) => {
  if (historyPtr.value < history.value.length) {
    history.value = history.value.slice(0, historyPtr.value)
  }
  history.value.push(action)
  historyPtr.value = history.value.length
}

const applyAction = (action: Action, direction: 'forward' | 'back') => {
  const nextValue = direction === 'forward' ? action.nextValue : action.prevValue
  const nextNotes = direction === 'forward' ? action.nextNotes : action.prevNotes
  cells.value[action.idx] = nextValue
  notes.value[action.idx] = nextNotes
}

const undo = () => {
  if (!canUndo.value) return
  const action = history.value[historyPtr.value - 1]
  applyAction(action, 'back')
  historyPtr.value--
}

const redo = () => {
  if (!canRedo.value) return
  const action = history.value[historyPtr.value]
  applyAction(action, 'forward')
  historyPtr.value++
}

// One line between neighbouring cells (the cell's own top/left edge); box borders are
// heavier and the board's outer right/bottom edge closes the grid.
const cellBorderStyle = (idx: number) => {
  const n = size.value
  const bs = boxSize.value
  const r = Math.floor(idx / n)
  const c = idx % n
  const boxTop = r % bs === 0
  const boxLeft = c % bs === 0
  return {
    borderTopWidth: boxTop ? '2px' : '1px',
    borderLeftWidth: boxLeft ? '2px' : '1px',
    borderRightWidth: c === n - 1 ? '2px' : '0',
    borderBottomWidth: r === n - 1 ? '2px' : '0',
    borderTopColor: boxTop ? 'var(--grid-strong)' : 'var(--grid-thin)',
    borderLeftColor: boxLeft ? 'var(--grid-strong)' : 'var(--grid-thin)',
    borderRightColor: 'var(--grid-strong)',
    borderBottomColor: 'var(--grid-strong)',
  }
}

const rowOf = (idx: number) => Math.floor(idx / size.value)
const colOf = (idx: number) => idx % size.value
const boxOf = (idx: number) => Math.floor(rowOf(idx) / boxSize.value) * boxSize.value + Math.floor(colOf(idx) / boxSize.value)

const isPeer = (idx: number, sel: number) => {
  if (idx === sel) return false
  return rowOf(idx) === rowOf(sel) || colOf(idx) === colOf(sel) || boxOf(idx) === boxOf(sel)
}

const isSameNumber = (idx: number, sel: number) => {
  const v = cells.value[sel] | 0
  if (!v) return false
  return idx !== sel && (cells.value[idx] | 0) === v
}

const canEdit = (idx: number) => !fixed.value[idx] && !solvedAt.value && !isSolved.value && !generating.value

const togglePop = ref<{ idx: number; nonce: number } | null>(null)
const pulseCell = (idx: number) => {
  togglePop.value = null
  requestAnimationFrame(() => {
    togglePop.value = { idx, nonce: Date.now() }
  })
}

const setCellValue = (idx: number, digit: number) => {
  if (!canEdit(idx)) return
  const prevValue = cells.value[idx] | 0
  const prevNotes = notes.value[idx] | 0

  const isEmpty = prevValue === 0
  if (notesMode.value && isEmpty && digit !== 0) {
    const bit = 1 << digit
    const nextNotes = prevNotes ^ bit
    if (nextNotes === prevNotes) return
    const action: Action = { idx, prevValue, nextValue: prevValue, prevNotes, nextNotes }
    pushAction(action)
    applyAction(action, 'forward')
    pulseCell(idx)
    return
  }

  const nextValue = digit === prevValue ? 0 : digit
  const nextNotes = 0
  if (nextValue === prevValue && nextNotes === prevNotes) return
  const action: Action = { idx, prevValue, nextValue, prevNotes, nextNotes }
  pushAction(action)
  applyAction(action, 'forward')
  pulseCell(idx)
}

const erase = () => {
  if (selected.value === null) return
  setCellValue(selected.value, 0)
}

const pressDigit = (digit: number) => {
  if (selected.value === null) return
  setCellValue(selected.value, digit)
}

const resetPuzzle = () => {
  if (!puzzle.value.length || generating.value) return
  cells.value = puzzle.value.slice()
  notes.value = new Array<number>(puzzle.value.length).fill(0)
  history.value = []
  historyPtr.value = 0
  hintsUsed.value = 0
  solvedAt.value = 0
  startedAt.value = Date.now()
  selected.value = null
  notesMode.value = false
}

const hint = () => {
  if (!solution.value.length || generating.value || solvedAt.value) return
  const empties: number[] = []
  const wrong: number[] = []
  for (let i = 0; i < cells.value.length; i++) {
    if (fixed.value[i]) continue
    const cur = cells.value[i] | 0
    const sol = solution.value[i] | 0
    if (cur === 0) empties.push(i)
    else if (cur !== sol) wrong.push(i)
  }
  const pool = empties.length ? empties : wrong
  if (!pool.length) return

  const idx = pool[Math.floor(Math.random() * pool.length)]
  const prevValue = cells.value[idx] | 0
  const prevNotes = notes.value[idx] | 0
  const nextValue = solution.value[idx] | 0
  const action: Action = { idx, prevValue, nextValue, prevNotes, nextNotes: 0 }
  pushAction(action)
  applyAction(action, 'forward')
  hintsUsed.value++
  selected.value = idx
  pulseCell(idx)
}

const newGame = async () => {
  if (generating.value) return
  generating.value = true
  generationError.value = ''
  solvedAt.value = 0
  selected.value = null
  await nextTick()
  await new Promise((r) => setTimeout(r, 0))

  try {
    const p = generateSudokuPuzzle(size.value, difficulty.value)
    puzzle.value = p.puzzle.slice()
    solution.value = p.solution.slice()
    cells.value = p.puzzle.slice()
    fixed.value = p.puzzle.map((v) => (v | 0) !== 0)
    notes.value = new Array<number>(p.puzzle.length).fill(0)
    history.value = []
    historyPtr.value = 0
    hintsUsed.value = 0
    notesMode.value = false
    startedAt.value = Date.now()
  } catch (e: any) {
    generationError.value = e?.message || 'Generation failed'
  } finally {
    generating.value = false
  }
}

const handleKeydown = (e: KeyboardEvent) => {
  if (generating.value) return
  const tag = (e.target as HTMLElement | null)?.tagName?.toLowerCase()
  if (tag === 'input' || tag === 'textarea' || tag === 'select') return
  if (e.key === 'Escape') {
    selected.value = null
    return
  }

  if (e.key === 'n' || e.key === 'N') {
    notesMode.value = !notesMode.value
    return
  }
  if (e.key === 'h' || e.key === 'H') {
    hint()
    return
  }
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'z') {
    if (e.shiftKey) redo()
    else undo()
    e.preventDefault()
    return
  }

  if (selected.value !== null) {
    const n = size.value
    const r = rowOf(selected.value)
    const c = colOf(selected.value)
    if (e.key === 'ArrowUp') {
      selected.value = ((r + n - 1) % n) * n + c
      e.preventDefault()
      return
    }
    if (e.key === 'ArrowDown') {
      selected.value = ((r + 1) % n) * n + c
      e.preventDefault()
      return
    }
    if (e.key === 'ArrowLeft') {
      selected.value = r * n + ((c + n - 1) % n)
      e.preventDefault()
      return
    }
    if (e.key === 'ArrowRight') {
      selected.value = r * n + ((c + 1) % n)
      e.preventDefault()
      return
    }
  }

  if (e.key === 'Backspace' || e.key === 'Delete') {
    erase()
    e.preventDefault()
    return
  }

  const digit = Number(e.key)
  if (Number.isInteger(digit) && digit >= 1 && digit <= size.value) {
    pressDigit(digit)
    e.preventDefault()
  }
}

onMounted(() => {
  clockTimer = window.setInterval(() => {
    clockNow.value = Date.now()
  }, 250)
  window.addEventListener('keydown', handleKeydown)
  void newGame()
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeydown)
  if (clockTimer) {
    window.clearInterval(clockTimer)
    clockTimer = null
  }
})

const solvedOverlayVisible = computed(() => !!solvedAt.value || isSolved.value)
watch(isSolved, (ok) => {
  if (ok && !solvedAt.value) {
    solvedAt.value = Date.now()
  }
})
watch(size, () => {
  void newGame()
})

const cellClass = (idx: number) => {
  const sel = selected.value
  const isSelected = sel === idx
  const peer = sel !== null ? isPeer(idx, sel) : false
  const same = sel !== null ? isSameNumber(idx, sel) : false
  const conflict = conflictMap.value[idx]
  const hinted = togglePop.value?.idx === idx
  return {
    selected: isSelected,
    peer,
    same,
    fixed: fixed.value[idx],
    filled: (cells.value[idx] | 0) !== 0,
    conflict,
    hinted,
  }
}

const hasNote = (idx: number, digit: number) => (notes.value[idx] & (1 << digit)) !== 0
const noteColumns = computed(() => boxSize.value)
</script>

<template>
  <div class="page game" :style="{ '--n': size }">
    <section class="card game-bar">
      <div class="game-title">
        <h2>{{ t('sudokuGame') }}</h2>
        <small>{{ t('sudokuGameSubtitle') }}</small>
      </div>
      <div class="game-controls">
        <label class="game-field">
          <span>{{ t('gameSize') }}</span>
          <select v-model.number="size" class="select">
            <option :value="4">4×4</option>
            <option :value="9">9×9</option>
          </select>
        </label>
        <label class="game-field">
          <span>{{ t('difficulty') }}</span>
          <select v-model="difficulty" class="select">
            <option value="easy">{{ t('easy') }}</option>
            <option value="normal">{{ t('normal') }}</option>
            <option value="hard">{{ t('hard') }}</option>
            <option value="expert">{{ t('expert') }}</option>
          </select>
        </label>
        <button class="btn primary" :disabled="generating" @click="newGame">
          <RefreshCw :size="14" aria-hidden="true" /><span>{{ t('newGame') }}</span>
        </button>
      </div>
    </section>

    <section class="game-layout">
      <div class="card board">
        <div class="grid">
          <button
            v-for="(_, idx) in cells"
            :key="idx"
            class="cell"
            :class="cellClass(idx)"
            :style="cellBorderStyle(idx)"
            :disabled="generating"
            @click="selected = idx"
          >
            <span v-if="(cells[idx] | 0) !== 0" class="digit">{{ cells[idx] }}</span>
            <span v-else class="notes" :style="{ '--cols': noteColumns }">
              <span v-for="d in digits" :key="d" class="note" :class="{ on: hasNote(idx, d) }">
                {{ hasNote(idx, d) ? d : '' }}
              </span>
            </span>
          </button>
        </div>

        <div v-if="generating" class="overlay">
          <div class="overlay-card">
            <div class="spinner" aria-hidden="true"></div>
            <strong>{{ t('generating') }}</strong>
            <small>{{ t('generatingHint') }}</small>
          </div>
        </div>

        <div v-if="generationError" class="overlay">
          <div class="overlay-card">
            <strong>{{ t('generationFailed') }}</strong>
            <small>{{ generationError }}</small>
            <button class="btn sm" @click="newGame">{{ t('tryAgain') }}</button>
          </div>
        </div>

        <div v-if="solvedOverlayVisible" class="overlay" @click.self="newGame">
          <div class="overlay-card">
            <span class="win-mark"><Check :size="22" :stroke-width="3" aria-hidden="true" /></span>
            <strong>{{ t('completed') }}</strong>
            <small>{{ t('time') }}: {{ elapsedText }} · {{ t('hintsUsed') }}: {{ hintsUsed }}</small>
            <div class="overlay-actions">
              <button class="btn sm" @click="resetPuzzle">{{ t('playAgain') }}</button>
              <button class="btn sm primary" @click="newGame">{{ t('newGame') }}</button>
            </div>
          </div>
        </div>
      </div>

      <aside class="card side">
        <div class="stats">
          <div class="stat"><small>{{ t('time') }}</small><strong class="num">{{ elapsedText }}</strong></div>
          <div class="stat"><small>{{ t('hintsUsed') }}</small><strong class="num">{{ hintsUsed }}</strong></div>
          <div class="stat"><small>{{ t('mode') }}</small><strong>{{ notesMode ? t('notes') : t('value') }}</strong></div>
        </div>

        <div class="keypad" :class="{ four: size === 4 }">
          <button v-for="d in digits" :key="d" class="key" :disabled="generating" @click="pressDigit(d)">{{ d }}</button>
        </div>

        <div class="tools">
          <button class="btn" :class="{ active: notesMode }" :aria-pressed="notesMode" @click="notesMode = !notesMode">
            <Pencil :size="14" aria-hidden="true" /><span>{{ t('notes') }}</span>
          </button>
          <button class="btn" :disabled="generating" @click="erase"><Eraser :size="14" aria-hidden="true" /><span>{{ t('erase') }}</span></button>
          <button class="btn" :disabled="generating || !!solvedAt" @click="hint"><Lightbulb :size="14" aria-hidden="true" /><span>{{ t('hint') }}</span></button>
          <button class="btn" :disabled="!canUndo" @click="undo"><Undo2 :size="14" aria-hidden="true" /><span>{{ t('undo') }}</span></button>
          <button class="btn" :disabled="!canRedo" @click="redo"><Redo2 :size="14" aria-hidden="true" /><span>{{ t('redo') }}</span></button>
          <button class="btn" :disabled="generating" @click="resetPuzzle"><RotateCcw :size="14" aria-hidden="true" /><span>{{ t('reset') }}</span></button>
        </div>

        <details class="kbd">
          <summary>{{ t('shortcuts') }}</summary>
          <div class="kbd-grid">
            <div class="kbd-row"><span>1-{{ size }}</span><span>{{ t('shortcutInput') }}</span></div>
            <div class="kbd-row"><span>Backspace</span><span>{{ t('shortcutErase') }}</span></div>
            <div class="kbd-row"><span>N</span><span>{{ t('shortcutNotes') }}</span></div>
            <div class="kbd-row"><span>H</span><span>{{ t('shortcutHint') }}</span></div>
            <div class="kbd-row"><span>Ctrl/⌘+Z</span><span>{{ t('shortcutUndo') }}</span></div>
            <div class="kbd-row"><span>Ctrl/⌘+Shift+Z</span><span>{{ t('shortcutRedo') }}</span></div>
          </div>
        </details>
      </aside>
    </section>
  </div>
</template>

<style scoped>
.game {
  --grid-strong: color-mix(in srgb, var(--ink) 70%, var(--surface));
  --grid-thin: var(--line-strong);
}

.game-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px 16px;
  padding: 12px 14px;
}

.game-title {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.game-title h2 {
  font-size: 14px;
  font-weight: 650;
}

.game-title small {
  color: var(--muted);
  font-size: 12px;
}

.game-controls {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 8px;
}

.game-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  color: var(--muted);
  font-size: 11px;
}

.game-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 12px;
  align-items: start;
}

@media (min-width: 880px) {
  .game-layout {
    grid-template-columns: minmax(0, 1fr) 300px;
  }
}

.board {
  position: relative;
  padding: 12px;
  overflow: hidden;
}

.grid {
  display: grid;
  grid-template-columns: repeat(var(--n), minmax(0, 1fr));
  width: min(540px, 100%);
  aspect-ratio: 1;
  margin: 0 auto;
  overflow: hidden;
  border: 0;
  border-radius: 8px;
  background: var(--surface);
  box-shadow: 0 0 0 2px var(--grid-strong);
}

.cell {
  position: relative;
  display: grid;
  place-items: center;
  width: 100%;
  min-width: 0;
  min-height: 0;
  padding: 0;
  overflow: hidden;
  border-style: solid;
  border-radius: 0;
  color: var(--ink);
  background: transparent;
  cursor: pointer;
  transition: background-color 0.12s;
}

.cell:disabled {
  cursor: default;
}

.cell.peer {
  background: color-mix(in srgb, var(--accent) 7%, transparent);
}

.cell.same {
  background: color-mix(in srgb, var(--accent) 18%, transparent);
}

.cell.selected {
  background: color-mix(in srgb, var(--accent) 24%, transparent);
  box-shadow: inset 0 0 0 2px var(--accent);
}

.cell.conflict {
  color: var(--bad);
  background: color-mix(in srgb, var(--bad) 16%, transparent);
}

.cell.hinted .digit {
  animation: pop 0.18s ease;
}

@keyframes pop {
  from {
    transform: scale(0.7);
  }
  to {
    transform: scale(1);
  }
}

.digit {
  font-size: clamp(16px, calc(min(540px, 100vw - 140px) / var(--n) * 0.52), 30px);
  font-weight: 650;
  line-height: 1;
}

.cell.filled:not(.fixed):not(.conflict) .digit {
  color: var(--accent-strong);
  font-weight: 550;
}

.cell.fixed .digit {
  font-weight: 750;
}

.notes {
  display: grid;
  grid-template-columns: repeat(var(--cols), 1fr);
  grid-template-rows: repeat(var(--cols), 1fr);
  width: 100%;
  height: 100%;
  padding: 2px;
  color: var(--muted);
  font-size: clamp(7px, calc(min(540px, 100vw - 140px) / var(--n) * 0.2), 11px);
  font-weight: 600;
  line-height: 1;
  user-select: none;
}

.note {
  display: grid;
  place-items: center;
}

.note.on {
  color: var(--ink-soft);
}

.overlay {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 12px;
  background: color-mix(in srgb, var(--surface) 82%, transparent);
}

.overlay-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  max-width: 320px;
  padding: 16px 18px;
  border: 1px solid var(--line-strong);
  border-radius: var(--radius-card);
  background: var(--surface);
  box-shadow: var(--shadow-pop);
  text-align: center;
}

.overlay-card small {
  color: var(--muted);
  font-size: 12px;
  overflow-wrap: anywhere;
}

.overlay-actions {
  display: flex;
  gap: 8px;
}

.spinner {
  width: 22px;
  height: 22px;
  border: 2.5px solid var(--line-strong);
  border-top-color: var(--accent);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

.win-mark {
  display: grid;
  place-items: center;
  width: 38px;
  height: 38px;
  border-radius: 50%;
  color: var(--accent-ink);
  background: var(--ok);
}

.side {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 12px;
}

.stats {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
}

.stat {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
  padding: 8px 10px;
  border-radius: var(--radius-control);
  background: var(--surface-2);
}

.stat small {
  overflow: hidden;
  color: var(--muted);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.stat strong {
  overflow: hidden;
  font-size: 15px;
  font-weight: 650;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.keypad {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 6px;
}

.keypad.four {
  grid-template-columns: repeat(4, minmax(0, 1fr));
}

.key {
  height: 40px;
  border: 1px solid var(--line-strong);
  border-radius: var(--radius-control);
  color: var(--ink);
  background: var(--surface);
  font-size: 16px;
  font-weight: 600;
  transition: background-color 0.12s;
}

.key:hover:not(:disabled) {
  background: var(--surface-2);
}

.key:active:not(:disabled) {
  background: var(--accent-soft);
}

.key:disabled {
  opacity: 0.45;
}

.tools {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(92px, 1fr));
  gap: 6px;
}

.tools .btn {
  min-width: 0;
  padding: 0 8px;
}

.tools .btn span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tools .btn.active {
  border-color: transparent;
  color: var(--accent-ink);
  background: var(--accent);
}

.kbd {
  padding-top: 10px;
  border-top: 1px solid var(--line);
  color: var(--ink-soft);
  font-size: 12px;
}

.kbd summary {
  color: var(--muted);
  cursor: pointer;
}

.kbd-grid {
  display: grid;
  gap: 6px;
  margin-top: 8px;
}

.kbd-row {
  display: grid;
  grid-template-columns: 1fr 1.3fr;
  gap: 8px;
}

.kbd-row span:first-child {
  font-family: var(--font-mono);
  font-size: 11px;
}
</style>
