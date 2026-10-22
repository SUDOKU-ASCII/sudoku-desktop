<script setup lang="ts">
import { computed } from 'vue'
import { ChevronDown, ClipboardPaste, X } from 'lucide-vue-next'
import { useApp } from '../../composables/appContext'
import { buildAsciiMode, parseAsciiMode, type ASCIIStyleToken } from '../../sudoku/asciiMode'

const {
  t,
  busy,
  editableNode,
  nodeEditorOpen,
  nodeEditorMode,
  shortlinkInput,
  closeNodeEditor,
  saveNode,
  parseShortlinkFromInput,
  parseShortlinkFromClipboard,
} = useApp()

const uplinkStyle = computed<ASCIIStyleToken>({
  get: () => parseAsciiMode(editableNode.ascii).uplink,
  set: (value) => {
    editableNode.ascii = buildAsciiMode({ uplink: value, downlink: parseAsciiMode(editableNode.ascii).downlink })
  },
})

const downlinkStyle = computed<ASCIIStyleToken>({
  get: () => parseAsciiMode(editableNode.ascii).downlink,
  set: (value) => {
    editableNode.ascii = buildAsciiMode({ uplink: parseAsciiMode(editableNode.ascii).uplink, downlink: value })
  },
})

const customTablesText = computed({
  get: () => editableNode.customTables.join('\n'),
  set: (value: string) => {
    editableNode.customTables = value.split('\n').map((x) => x.trim()).filter(Boolean)
  },
})

const canSave = computed(() => !!editableNode.serverAddress.trim() && !!editableNode.key.trim())
</script>

<template>
  <Transition name="modal">
    <div v-if="nodeEditorOpen" class="modal-overlay" @mousedown.self="closeNodeEditor">
      <form class="modal" @submit.prevent="canSave && saveNode()" @keydown.esc="closeNodeEditor">
        <div class="modal-head">
          <h2>{{ nodeEditorMode === 'create' ? t('addNode') : t('editNode') }}</h2>
          <button class="icon-btn" type="button" :title="t('close')" @click="closeNodeEditor"><X :size="16" aria-hidden="true" /></button>
        </div>

        <div class="modal-body">
          <div v-if="nodeEditorMode === 'create'" class="import-box">
            <div class="input-group">
              <input v-model="shortlinkInput" class="mono" placeholder="sudoku://…" spellcheck="false" @keydown.enter.prevent="parseShortlinkFromInput" />
              <button class="btn sm" type="button" :disabled="!shortlinkInput.trim()" @click="parseShortlinkFromInput">{{ t('parseShortlink') }}</button>
            </div>
            <button class="btn" type="button" @click="parseShortlinkFromClipboard"><ClipboardPaste :size="15" aria-hidden="true" />{{ t('parseFromClipboard') }}</button>
          </div>

          <div class="fields">
            <label class="field wide"><span>{{ t('name') }}</span><input v-model="editableNode.name" class="input" :placeholder="t('nodeNamePreferredPlaceholder')" /></label>
            <label class="field wide"><span>{{ t('server') }}</span><input v-model="editableNode.serverAddress" class="input mono" placeholder="host:port" spellcheck="false" /></label>
            <label class="field wide"><span>{{ t('key') }}</span><input v-model="editableNode.key" class="input mono" spellcheck="false" autocomplete="off" /></label>
          </div>

          <details class="card">
            <summary>{{ t('advancedOptions') }}<ChevronDown :size="16" aria-hidden="true" /></summary>
            <div class="card-body">
              <div class="fields">
                <label class="field"><span>AEAD</span>
                  <select v-model="editableNode.aead" class="select"><option>chacha20-poly1305</option><option>aes-128-gcm</option><option>none</option></select>
                </label>
                <label class="field"><span>{{ t('multiplex') }}</span>
                  <select v-model="editableNode.multiplex" class="select"><option>auto</option><option>on</option><option>off</option></select>
                </label>
                <label class="field"><span>{{ t('uplinkStyle') }}</span>
                  <select v-model="uplinkStyle" class="select"><option value="entropy">{{ t('styleEntropy') }}</option><option value="ascii">{{ t('styleAscii') }}</option></select>
                </label>
                <label class="field"><span>{{ t('downlinkStyle') }}</span>
                  <select v-model="downlinkStyle" class="select"><option value="entropy">{{ t('styleEntropy') }}</option><option value="ascii">{{ t('styleAscii') }}</option></select>
                </label>
                <label class="field"><span>{{ t('paddingMin') }}</span><input v-model.number="editableNode.paddingMin" class="input" type="number" min="0" /></label>
                <label class="field"><span>{{ t('paddingMax') }}</span><input v-model.number="editableNode.paddingMax" class="input" type="number" min="0" /></label>
              </div>
              <label class="inline-row">
                <div class="row-text"><strong>{{ t('pureDownlink') }}</strong><small>{{ t('pureDownlinkHint') }}</small></div>
                <span class="switch"><input v-model="editableNode.enablePureDownlink" type="checkbox" /><i /></span>
              </label>

              <div class="section-title">HTTPMask</div>
              <label class="inline-row">
                <div class="row-text"><strong>{{ t('httpMaskEnabled') }}</strong></div>
                <span class="switch"><input :checked="!editableNode.httpMask.disable" type="checkbox" @change="editableNode.httpMask.disable = !($event.target as HTMLInputElement).checked" /><i /></span>
              </label>
              <div v-if="!editableNode.httpMask.disable" class="fields">
                <label class="field"><span>{{ t('mode') }}</span>
                  <select v-model="editableNode.httpMask.mode" class="select"><option>auto</option><option>legacy</option><option>stream</option><option>poll</option><option>ws</option></select>
                </label>
                <label class="field"><span>TLS</span>
                  <select v-model="editableNode.httpMask.tls" class="select"><option :value="true">{{ t('enabled') }}</option><option :value="false">{{ t('disabled') }}</option></select>
                </label>
                <label class="field"><span>{{ t('host') }}</span><input v-model="editableNode.httpMask.host" class="input mono" placeholder="example.com" spellcheck="false" /></label>
                <label class="field"><span>{{ t('pathRoot') }}</span><input v-model="editableNode.httpMask.pathRoot" class="input mono" spellcheck="false" /></label>
              </div>

              <label class="field"><span>{{ t('customTablesPerLine') }}</span>
                <textarea v-model.lazy="customTablesText" class="textarea mono" rows="3" spellcheck="false" />
              </label>
            </div>
          </details>
        </div>

        <div class="modal-foot">
          <button class="btn" type="button" @click="closeNodeEditor">{{ t('cancel') }}</button>
          <button class="btn primary" type="submit" :disabled="busy || !canSave">{{ t('save') }}</button>
        </div>
      </form>
    </div>
  </Transition>
</template>
