<script setup lang="ts">
import type { NodeConfig } from '../../types'

const props = defineProps<{
  open: boolean
  nodeEditorMode: 'create' | 'edit'
  editableNode: NodeConfig
  shortlinkInput: string
  shortlinkName: string
  t: (key: any) => string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'save'): void
  (e: 'parse-shortlink'): void
  (e: 'parse-clipboard'): void
  (e: 'update:shortlinkInput', value: string): void
  (e: 'update:shortlinkName', value: string): void
}>()
</script>

<template>
  <div v-if="props.open" class="modal-overlay" @click.self="emit('close')">
    <section class="modal-card">
      <header class="modal-head">
        <h3>{{ props.nodeEditorMode === 'create' ? props.t('addNode') : props.t('editNode') }}</h3>
        <button class="iconbtn" @click="emit('close')" :title="props.t('close')">
          <svg viewBox="0 0 24 24"><path d="M6 6l12 12M18 6L6 18" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/></svg>
        </button>
      </header>

      <div class="modal-content">
        <section class="group-card">
          <h4>{{ props.t('shortlinkQuickImport') }}</h4>
          <div class="form-grid compact-grid">
            <label class="field"><span>{{ props.t('nodeName') }}</span><input :value="props.shortlinkName" :placeholder="props.t('nodeNamePreferredPlaceholder')" @input="emit('update:shortlinkName', ($event.target as HTMLInputElement).value)" /></label>
            <label class="field span-2"><span>{{ props.t('shortlink') }}</span><textarea :value="props.shortlinkInput" rows="3" placeholder="sudoku://..." @input="emit('update:shortlinkInput', ($event.target as HTMLTextAreaElement).value)" /></label>
          </div>
          <div class="row">
            <button class="btn ghost" @click="emit('parse-shortlink')">{{ props.t('parseShortlink') }}</button>
            <button class="btn ghost" @click="emit('parse-clipboard')">{{ props.t('parseFromClipboard') }}</button>
          </div>
        </section>

        <section class="group-card">
          <h4>{{ props.t('basicConfig') }}</h4>
          <div class="form-grid compact-grid">
            <label class="field"><span>{{ props.t('name') }}</span><input v-model="props.editableNode.name" /></label>
            <label class="field"><span>{{ props.t('server') }}</span><input v-model="props.editableNode.serverAddress" placeholder="host:port" /></label>
            <label class="field span-2"><span>{{ props.t('key') }}</span><textarea v-model="props.editableNode.key" rows="3" /></label>
            <label class="field"><span>AEAD</span><select v-model="props.editableNode.aead"><option>chacha20-poly1305</option><option>aes-128-gcm</option><option>none</option></select></label>
            <label class="field"><span>ASCII</span><select v-model="props.editableNode.ascii"><option>prefer_entropy</option><option>prefer_ascii</option></select></label>
            <label class="field"><span>{{ props.t('localPort') }}</span><input v-model.number="props.editableNode.localPort" type="number" /></label>
            <label class="field"><span>{{ props.t('paddingMin') }}</span><input v-model.number="props.editableNode.paddingMin" type="number" /></label>
            <label class="field"><span>{{ props.t('paddingMax') }}</span><input v-model.number="props.editableNode.paddingMax" type="number" /></label>
            <label class="switch-row compact"><span>{{ props.t('enableNode') }}</span><span class="switch-control"><input type="checkbox" v-model="props.editableNode.enabled" /><span class="switch-ui" /></span></label>
            <label class="switch-row compact"><span>{{ props.t('pureDownlink') }}</span><span class="switch-control"><input type="checkbox" v-model="props.editableNode.enablePureDownlink" /><span class="switch-ui" /></span></label>
          </div>
        </section>

        <section class="group-card">
          <h4>HTTPMask</h4>
          <div class="form-grid compact-grid">
            <label class="switch-row compact"><span>{{ props.t('disable') }}</span><span class="switch-control"><input type="checkbox" v-model="props.editableNode.httpMask.disable" /><span class="switch-ui" /></span></label>
            <label class="field"><span>{{ props.t('mode') }}</span>
              <select v-model="props.editableNode.httpMask.mode">
                <option>legacy</option>
                <option>stream</option>
                <option>poll</option>
                <option>auto</option>
                <option>ws</option>
              </select>
            </label>
            <label class="switch-row compact"><span>TLS</span><span class="switch-control"><input type="checkbox" v-model="props.editableNode.httpMask.tls" /><span class="switch-ui" /></span></label>
            <label class="field"><span>{{ props.t('host') }}</span><input v-model="props.editableNode.httpMask.host" placeholder="example.com" /></label>
            <label class="field"><span>{{ props.t('pathRoot') }}</span><input v-model="props.editableNode.httpMask.pathRoot" placeholder="aabbcc" /></label>
            <label class="field"><span>{{ props.t('multiplex') }}</span>
              <select v-model="props.editableNode.httpMask.multiplex">
                <option>auto</option>
                <option>on</option>
                <option>off</option>
              </select>
            </label>
          </div>
        </section>

        <section class="group-card">
          <h4>{{ props.t('advancedOptions') }}</h4>
          <div class="form-grid compact-grid">
            <label class="field"><span>{{ props.t('customTable') }}</span><input v-model="props.editableNode.customTable" /></label>
            <label class="field span-2"><span>{{ props.t('customTablesPerLine') }}</span>
              <textarea
                :value="props.editableNode.customTables.join('\n')"
                rows="3"
                @input="props.editableNode.customTables = ($event.target as HTMLTextAreaElement).value.split('\n').map((x) => x.trim()).filter(Boolean)"
              />
            </label>
          </div>
        </section>
      </div>

      <footer class="modal-foot">
        <button class="btn ghost" @click="emit('close')">{{ props.t('cancel') }}</button>
        <button class="btn" @click="emit('save')">{{ props.t('save') }}</button>
      </footer>
    </section>
  </div>
</template>
