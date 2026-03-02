<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { generateSudokuPuzzle, type SudokuDifficulty, type SudokuSize } from '../sudoku/engine'
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

const cellBorderStyle = (idx: number) => {
  const n = size.value
  const bs = boxSize.value
  const r = Math.floor(idx / n)
  const c = idx % n
  return {
    borderTopWidth: r % bs === 0 ? '4px' : '2px',
    borderLeftWidth: c % bs === 0 ? '4px' : '2px',
    borderRightWidth: c === n - 1 ? '4px' : '2px',
    borderBottomWidth: r === n - 1 ? '4px' : '2px',
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
  <div class="game-root">
    <header class="game-head">
      <div class="game-title">
        <h3>{{ t('sudokuGame') }}</h3>
        <p>{{ t('sudokuGameSubtitle') }}</p>
      </div>

      <div class="game-toolbar">
        <label class="select">
          <span>{{ t('gameSize') }}</span>
          <select v-model.number="size">
            <option :value="4">4×4</option>
            <option :value="9">9×9</option>
          </select>
        </label>
        <label class="select">
          <span>{{ t('difficulty') }}</span>
          <select v-model="difficulty">
            <option value="easy">{{ t('easy') }}</option>
            <option value="normal">{{ t('normal') }}</option>
            <option value="hard">{{ t('hard') }}</option>
            <option value="expert">{{ t('expert') }}</option>
          </select>
        </label>

        <button class="btn" :disabled="generating" @click="newGame">
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path
              d="M20 12a8 8 0 0 1-14.9 4M4 12A8 8 0 0 1 18.9 8"
              fill="none"
              stroke="currentColor"
              stroke-width="2.6"
              stroke-linecap="round"
            />
            <path
              d="M18 3v5h-5"
              fill="none"
              stroke="currentColor"
              stroke-width="2.6"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
          <span>{{ t('newGame') }}</span>
        </button>
      </div>
    </header>

    <section class="game-layout">
      <div class="board brutal-card">
        <div class="grid" :style="{ '--n': size }">
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
          <div class="overlay-card brutal-card">
            <div class="spinner" aria-hidden="true"></div>
            <strong>{{ t('generating') }}</strong>
            <small>{{ t('generatingHint') }}</small>
          </div>
        </div>

        <div v-if="generationError" class="overlay error">
          <div class="overlay-card brutal-card">
            <strong>{{ t('generationFailed') }}</strong>
            <small>{{ generationError }}</small>
            <button class="btn mini" @click="newGame">{{ t('tryAgain') }}</button>
          </div>
        </div>

        <div v-if="solvedOverlayVisible" class="overlay success" @click.self="newGame">
          <div class="overlay-card brutal-card win">
            <svg class="check" viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M5 13l4 4L19 7"
                fill="none"
                stroke="currentColor"
                stroke-width="2.8"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
            </svg>
            <strong>{{ t('completed') }}</strong>
            <small>{{ t('time') }}: {{ elapsedText }} · {{ t('hintsUsed') }}: {{ hintsUsed }}</small>
            <div class="row">
              <button class="btn mini" @click="resetPuzzle">{{ t('playAgain') }}</button>
              <button class="btn mini" @click="newGame">{{ t('newGame') }}</button>
            </div>
          </div>
        </div>
      </div>

      <aside class="side brutal-card">
        <div class="stats">
          <div class="stat">
            <small>{{ t('time') }}</small>
            <strong>{{ elapsedText }}</strong>
          </div>
          <div class="stat">
            <small>{{ t('hintsUsed') }}</small>
            <strong>{{ hintsUsed }}</strong>
          </div>
          <div class="stat">
            <small>{{ t('mode') }}</small>
            <strong>{{ notesMode ? t('notes') : t('value') }}</strong>
          </div>
        </div>

        <div class="tools">
          <button class="btn mini" :class="{ active: notesMode }" @click="notesMode = !notesMode">
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M3 17.5V21h3.5L19 8.5 15.5 5 3 17.5Z"
                fill="none"
                stroke="currentColor"
                stroke-width="2.4"
                stroke-linejoin="round"
              />
              <path d="M14.5 6L18 9.5" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" />
            </svg>
            <span>{{ t('notes') }}</span>
          </button>

          <button class="btn mini" :disabled="!canUndo" @click="undo">
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M9 14L4 9l5-5"
                fill="none"
                stroke="currentColor"
                stroke-width="2.6"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
              <path
                d="M20 20v-5a7 7 0 0 0-7-7H4"
                fill="none"
                stroke="currentColor"
                stroke-width="2.6"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
            </svg>
            <span>{{ t('undo') }}</span>
          </button>

          <button class="btn mini" :disabled="!canRedo" @click="redo">
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M15 4l5 5-5 5"
                fill="none"
                stroke="currentColor"
                stroke-width="2.6"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
              <path
                d="M4 20v-5a7 7 0 0 1 7-7h9"
                fill="none"
                stroke="currentColor"
                stroke-width="2.6"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
            </svg>
            <span>{{ t('redo') }}</span>
          </button>
        </div>

        <div class="keypad">
          <button v-for="d in digits" :key="d" class="key" :disabled="generating" @click="pressDigit(d)">
            {{ d }}
          </button>

          <button class="key wide" :disabled="generating" @click="erase">
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M6 19h12M7 7l10 10M17 7 7 17"
                fill="none"
                stroke="currentColor"
                stroke-width="2.6"
                stroke-linecap="round"
              />
            </svg>
            <span>{{ t('erase') }}</span>
          </button>

          <button class="key wide" :disabled="generating || !!solvedAt" @click="hint">
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M12 2a7 7 0 0 0-4 12v3h8v-3a7 7 0 0 0-4-12Z"
                fill="none"
                stroke="currentColor"
                stroke-width="2.6"
                stroke-linejoin="round"
              />
              <path d="M9 22h6" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" />
            </svg>
            <span>{{ t('hint') }}</span>
          </button>

          <button class="key wide" :disabled="generating" @click="resetPuzzle">
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M20 12a8 8 0 0 1-8 8 8 8 0 0 1-7.1-4.3"
                fill="none"
                stroke="currentColor"
                stroke-width="2.6"
                stroke-linecap="round"
              />
              <path d="M6 16H3v3" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
            <span>{{ t('reset') }}</span>
          </button>
        </div>

        <div class="kbd brutal-card">
          <div class="kbd-title">{{ t('shortcuts') }}</div>
          <div class="kbd-grid">
            <div class="kbd-row"><span>1-{{ size }}</span><span>{{ t('shortcutInput') }}</span></div>
            <div class="kbd-row"><span>Backspace</span><span>{{ t('shortcutErase') }}</span></div>
            <div class="kbd-row"><span>N</span><span>{{ t('shortcutNotes') }}</span></div>
            <div class="kbd-row"><span>H</span><span>{{ t('shortcutHint') }}</span></div>
            <div class="kbd-row"><span>Ctrl/⌘+Z</span><span>{{ t('shortcutUndo') }}</span></div>
            <div class="kbd-row"><span>Ctrl/⌘+Shift+Z</span><span>{{ t('shortcutRedo') }}</span></div>
          </div>
        </div>
      </aside>
    </section>
  </div>
</template>

<style scoped>
.game-root {
  display: grid;
  gap: 14px;
}

.game-head {
  display: grid;
  grid-template-columns: 1.1fr auto;
  gap: 12px;
  align-items: start;
}

.game-title h3 {
  margin: 0;
  font-size: 18px;
  letter-spacing: 0.2px;
}

.game-title p {
  margin: 6px 0 0;
  color: var(--ink-soft);
}

.game-toolbar {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: flex-end;
  align-items: flex-end;
}

.select {
  display: grid;
  gap: 6px;
  font-size: 12px;
  font-weight: 800;
}

.select span {
  opacity: 0.8;
}

.select select {
  border: 3px solid var(--ink);
  border-radius: 10px;
  background: var(--paper);
  padding: 6px 10px;
  font-weight: 800;
}

.btn svg {
  width: 18px;
  height: 18px;
}

.btn {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.btn.active {
  background: var(--ink);
  color: var(--paper);
}

.game-layout {
  display: grid;
  grid-template-columns: minmax(420px, 1fr) minmax(280px, 360px);
  gap: 14px;
  align-items: start;
}

@media (max-width: 1100px) {
  .game-head {
    grid-template-columns: 1fr;
  }
  .game-toolbar {
    justify-content: flex-start;
  }
  .game-layout {
    grid-template-columns: 1fr;
  }
}

.board {
  position: relative;
  padding: 14px;
  min-height: 520px;
  overflow: hidden;
}

.grid {
  display: grid;
  grid-template-columns: repeat(var(--n), 1fr);
  width: min(560px, 100%);
  aspect-ratio: 1;
  margin: 0 auto;
  background: var(--paper);
  border-radius: 16px;
  box-shadow: inset 0 0 0 3px var(--ink);
}

.cell {
  appearance: none;
  border-style: solid;
  border-color: var(--ink);
  background: transparent;
  color: var(--ink);
  cursor: pointer;
  padding: 0;
  display: grid;
  place-items: center;
  transition:
    transform 0.12s ease,
    background-color 0.12s ease,
    color 0.12s ease,
    box-shadow 0.12s ease;
}

.cell:hover {
  transform: translateY(-1px);
}

.cell:disabled {
  cursor: not-allowed;
}

.cell.peer {
  background: color-mix(in srgb, var(--accent-b) 18%, transparent);
}

.cell.same {
  background: color-mix(in srgb, var(--accent-c) 18%, transparent);
}

.cell.selected {
  background: color-mix(in srgb, var(--accent-a) 20%, transparent);
  box-shadow: inset 0 0 0 3px var(--ink);
}

.cell.fixed .digit {
  font-weight: 900;
}

.cell.conflict {
  background: color-mix(in srgb, var(--bad) 22%, transparent);
  color: var(--bad);
}

.cell.hinted {
  animation: pop 0.16s ease;
}

@keyframes pop {
  from {
    transform: scale(0.92);
  }
  to {
    transform: scale(1);
  }
}

.digit {
  font-weight: 900;
  font-size: clamp(18px, 2.2vw, 28px);
  line-height: 1;
}

.notes {
  width: 100%;
  height: 100%;
  display: grid;
  grid-template-columns: repeat(var(--cols), 1fr);
  grid-template-rows: repeat(var(--cols), 1fr);
  padding: 6px;
  gap: 2px;
  align-items: center;
  justify-items: center;
  font-size: 11px;
  font-weight: 800;
  color: var(--ink-soft);
  user-select: none;
}

.note {
  width: 100%;
  height: 100%;
  display: grid;
  place-items: center;
  border-radius: 6px;
  transition: background-color 0.12s ease;
}

.note.on {
  background: color-mix(in srgb, var(--accent-d) 22%, transparent);
  color: var(--ink);
}

.overlay {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  background: color-mix(in srgb, var(--paper-soft) 50%, transparent);
  backdrop-filter: blur(6px);
  animation: fade 0.18s ease;
}

.overlay.error {
  background: color-mix(in srgb, #ffe0dc 55%, transparent);
}

.overlay.success {
  background: color-mix(in srgb, #dffbe9 55%, transparent);
}

@keyframes fade {
  from {
    opacity: 0.2;
  }
  to {
    opacity: 1;
  }
}

.overlay-card {
  padding: 14px 16px;
  display: grid;
  gap: 8px;
  text-align: center;
  max-width: 360px;
}

.overlay-card.win {
  max-width: 420px;
}

.overlay-card small {
  color: var(--ink-soft);
}

.spinner {
  width: 22px;
  height: 22px;
  border: 3px solid var(--ink);
  border-right-color: transparent;
  border-radius: 999px;
  margin: 0 auto;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}

.check {
  width: 34px;
  height: 34px;
  margin: 0 auto;
}

.side {
  padding: 14px;
  display: grid;
  gap: 12px;
}

.stats {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
}

.stat {
  border: 3px solid var(--ink);
  border-radius: 12px;
  background: var(--paper);
  padding: 10px;
}

.stat small {
  display: block;
  opacity: 0.75;
  font-weight: 900;
  letter-spacing: 0.2px;
  font-size: 11px;
}

.stat strong {
  display: block;
  margin-top: 6px;
  font-weight: 900;
  font-size: 16px;
}

.tools {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.keypad {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
}

.key {
  border: 3px solid var(--ink);
  border-radius: 12px;
  background: var(--paper);
  padding: 10px 10px;
  font-weight: 900;
  cursor: pointer;
  transition: transform 0.12s ease;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
}

.key svg {
  width: 18px;
  height: 18px;
}

.key:hover {
  transform: translateY(-2px);
}

.key:disabled {
  opacity: 0.55;
  cursor: not-allowed;
  transform: none;
}

.wide {
  grid-column: span 3;
  justify-content: center;
}

.kbd {
  padding: 12px;
  box-shadow: none;
  border-width: 2px;
}

.kbd-title {
  font-weight: 900;
  font-size: 12px;
  letter-spacing: 0.3px;
  margin-bottom: 8px;
}

.kbd-grid {
  display: grid;
  gap: 6px;
  font-size: 12px;
}

.kbd-row {
  display: grid;
  grid-template-columns: 1fr 1.3fr;
  gap: 8px;
}

.kbd-row span:first-child {
  font-weight: 900;
}

.kbd-row span:last-child {
  color: var(--ink-soft);
}
</style>
