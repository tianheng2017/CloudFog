<script setup lang="ts">
// 管理 · 目录管理（07 §4.2）：模型 / 定价 / 映射三个 tab 全写操作。
// 契约要点：金额全部十进制字符串；model-prices 的 model_id 为必填 query；
// price 响应不回显 cache 单价（编辑时保持现值不传 cache 字段，需调整请删除重建）。
definePageMeta({ layout: 'admin' })
const activeTab = ref('models')

interface ProviderItem { code: string; name: string; protocol: string; base_url: string; status: string }
interface ModelItem {
  id: number; name: string; provider_code: string; display_name: string
  context_window: number; max_output_tokens: number; capabilities: string[]
  billing_mode: string; fallbacks: string[]; status: string
}
interface PriceItem {
  id: number; model_id: number; currency: string
  input_price_per_1k: string; output_price_per_1k: string; per_request_price: string
  effective_from: string; effective_to: string | null
}
interface MappingItem { id: number; channel_id: number | null; alias: string; upstream_model: string; priority: number }

const { data: providers } = useApiResource<{ items: ProviderItem[] }>(
  () => apiFetch('/api/v1/admin/providers'), { items: [] })

const CAPS = ['stream', 'tools', 'vision', 'reasoning', 'json_mode', 'audio', 'image_gen']
const BMODES = ['token', 'per_request', 'image']
const capLabel: Record<string, string> = {
  stream: '流式', tools: '工具调用', vision: '视觉', reasoning: '推理', json_mode: 'JSON 模式', audio: '音频', image_gen: '生图',
}

// ════════ Tab 1：模型 ════════
const modelProvider = ref('')
const { data: models, loading: modelsLoading, error: modelsError, reload: reloadModels } = useApiResource<{ items: ModelItem[] }>(
  () => {
    const q = modelProvider.value ? `?provider_code=${encodeURIComponent(modelProvider.value)}` : ''
    return apiFetch(`/api/v1/admin/models${q}`)
  },
  { items: [] })

const modelDialogOpen = ref(false)
const modelEditing = ref(false)
const modelSaving = ref(false)
const modelEditId = ref(0)
const modelForm = reactive({
  name: '', provider_code: '', display_name: '', context_window: 8192, max_output_tokens: 4096,
  capabilities: [] as string[], billing_mode: 'token', fallbacks: '', status: 'active',
})
function openModelDialog(m?: ModelItem) {
  modelEditing.value = !!m
  modelEditId.value = m?.id ?? 0
  Object.assign(modelForm, m ? {
    name: m.name, provider_code: m.provider_code, display_name: m.display_name || '',
    context_window: m.context_window, max_output_tokens: m.max_output_tokens,
    capabilities: [...(m.capabilities || [])], billing_mode: m.billing_mode || 'token',
    fallbacks: (m.fallbacks || []).join('\n'), status: m.status,
  } : {
    name: '', provider_code: providers.value.items[0]?.code || '', display_name: '', context_window: 8192,
    max_output_tokens: 4096, capabilities: [], billing_mode: 'token', fallbacks: '', status: 'active',
  })
  modelDialogOpen.value = true
}
async function saveModel() {
  const f = modelForm
  if (!f.name.trim() || !f.provider_code) { ElMessage.warning('模型名与供应商必填'); return }
  modelSaving.value = true
  try {
    const body = {
      name: f.name.trim(), provider_code: f.provider_code, display_name: f.display_name.trim(),
      context_window: f.context_window, max_output_tokens: f.max_output_tokens,
      capabilities: f.capabilities, billing_mode: f.billing_mode, status: f.status,
      fallbacks: f.fallbacks.split('\n').map(x => x.trim()).filter(Boolean),
    }
    if (modelEditing.value) {
      await apiFetch(`/api/v1/admin/models/${modelEditId.value}`, { method: 'PATCH', body })
    } else {
      await apiFetch('/api/v1/admin/models', { method: 'POST', body })
    }
    modelDialogOpen.value = false
    ElMessage.success(modelEditing.value ? '模型已更新' : '模型已创建')
    void reloadModels()
    if (activeTab.value === 'prices') void reloadPrices()
  } catch (e: any) { ElMessage.error(apiMessage(e, '保存失败')) } finally { modelSaving.value = false }
}
function openModelEdit(m: ModelItem) { openModelDialog(m) }
async function deleteModel(m: ModelItem) {
  try {
    await ElMessageBox.confirm(`删除模型「${m.name}」？历史价格与用量保留，目录不再对外。`, '删除模型', { type: 'warning', confirmButtonText: '删除' })
  } catch { return }
  try {
    await apiFetch(`/api/v1/admin/models/${m.id}`, { method: 'DELETE' })
    ElMessage.success('已删除')
    void reloadModels()
  } catch (e: any) { ElMessage.error(apiMessage(e, '删除失败')) }
}

// ════════ Tab 2：定价（model_id 必填）════════
const priceModelId = ref<number | null>(null)
const { data: prices, loading: pricesLoading, error: pricesError, reload: reloadPrices } = useApiResource<{ items: PriceItem[] }>(
  () => apiFetch(`/api/v1/admin/model-prices?model_id=${priceModelId.value ?? 0}`),
  { items: [] })
function watchModelForPrice(id: number) {
  priceModelId.value = id
  void reloadPrices()
}
const priceFormOpen = ref(false)
const priceEditing = ref(false)
const priceSaving = ref(false)
const priceForm = reactive({
  currency: 'USD', input: '', output: '', cache_read: '', cache_write: '', per_request: '', effective_from: '', effective_to: '',
})
function openPriceDialog(p?: PriceItem) {
  priceEditing.value = !!p
  Object.assign(priceForm, p ? {
    currency: p.currency || 'USD', input: p.input_price_per_1k, output: p.output_price_per_1k,
    cache_read: '', cache_write: '', per_request: p.per_request_price,
    effective_from: p.effective_from.slice(0, 10), effective_to: p.effective_to ? p.effective_to.slice(0, 10) : '',
  } : {
    currency: 'USD', input: '', output: '', cache_read: '', cache_write: '', per_request: '', effective_from: '', effective_to: '',
  })
  priceFormOpen.value = true
}
function validMoney(v: string): boolean { return v === '' || /^\d+(\.\d+)?$/.test(v) }
async function savePrice() {
  if (priceModelId.value == null) { ElMessage.warning('请先选择模型'); return }
  for (const k of ['input', 'output', 'cache_read', 'cache_write', 'per_request'] as const) {
    if (!validMoney(priceForm[k])) { ElMessage.warning('价格须为非负十进制数'); return }
  }
  priceSaving.value = true
  try {
    const f = priceForm
    // 新建：可为空字段赋零值（服务端空串→0）；编辑：只改 input/output/per_request/currency/effective_to
    //（价格响应不含 cache 现值 → 编辑时保持现值，不传 cache_*；effective_from 亦不可改以防误撞唯一键）
    const body: Record<string, any> = { currency: f.currency || 'USD' }
    if (priceEditing.value) {
      body.input_price_per_1k = f.input === '' ? '0' : f.input
      body.output_price_per_1k = f.output === '' ? '0' : f.output
      body.per_request_price = f.per_request === '' ? '0' : f.per_request
      if (f.effective_to) body.effective_to = f.effective_to
      await apiFetch(`/api/v1/admin/model-prices/${priceEditId.value}`, { method: 'PATCH', body })
    } else {
      body.input_price_per_1k = f.input
      body.output_price_per_1k = f.output
      body.per_request_price = f.per_request
      if (f.cache_read) body.cache_read_price_per_1k = f.cache_read
      if (f.cache_write) body.cache_write_price_per_1k = f.cache_write
      if (f.effective_from) body.effective_from = f.effective_from
      if (f.effective_to) body.effective_to = f.effective_to
      await apiFetch(`/api/v1/admin/model-prices?model_id=${priceModelId.value}`, { method: 'POST', body })
    }
    priceFormOpen.value = false
    ElMessage.success(priceEditing.value ? '价格已更新' : '价格已创建')
    void reloadPrices()
  } catch (e: any) { ElMessage.error(apiMessage(e, '保存失败')) } finally { priceSaving.value = false }
}
const priceEditId = ref(0)
function openPriceEdit(p: PriceItem) { priceEditId.value = p.id; openPriceDialog(p) }
async function deletePrice(p: PriceItem) {
  try {
    await ElMessageBox.confirm(`删除 ${p.effective_from.slice(0, 10)} 生效的价格档？`, '删除价格', { type: 'warning', confirmButtonText: '删除' })
  } catch { return }
  try {
    await apiFetch(`/api/v1/admin/model-prices/${p.id}`, { method: 'DELETE' })
    void reloadPrices()
  } catch (e: any) { ElMessage.error(apiMessage(e, '删除失败')) }
}
function fmtPriceDate(s: string) { return formatDate(s) }

// ════════ Tab 3：映射 ════════
const aliasFilter = ref('')
const { data: mappings, loading: mappingsLoading, error: mappingsError, reload: reloadMappings } = useApiResource<{ items: MappingItem[] }>(
  () => {
    const q = aliasFilter.value ? `?alias=${encodeURIComponent(aliasFilter.value)}` : ''
    return apiFetch(`/api/v1/admin/model-mappings${q}`)
  },
  { items: [] })
const mappingOpen = ref(false)
const mappingSaving = ref(false)
const mappingForm = reactive({ alias: '', upstream_model: '', scope: 'global', channel_id: undefined as number | undefined, priority: 0 })
function openMappingDialog() {
  Object.assign(mappingForm, { alias: '', upstream_model: '', scope: 'global', channel_id: undefined, priority: 0 })
  mappingOpen.value = true
}
async function saveMapping() {
  if (!mappingForm.alias.trim() || !mappingForm.upstream_model.trim()) { ElMessage.warning('alias 与上游模型名必填'); return }
  mappingSaving.value = true
  try {
    const body: Record<string, any> = {
      alias: mappingForm.alias.trim(), upstream_model: mappingForm.upstream_model.trim(), priority: mappingForm.priority || 0,
    }
    if (mappingForm.scope === 'channel' && mappingForm.channel_id != null) body.channel_id = mappingForm.channel_id
    await apiFetch('/api/v1/admin/model-mappings', { method: 'POST', body })
    mappingOpen.value = false
    ElMessage.success('映射已创建')
    void reloadMappings()
  } catch (e: any) { ElMessage.error(apiMessage(e, '创建失败')) } finally { mappingSaving.value = false }
}
async function deleteMapping(m: MappingItem) {
  try {
    await ElMessageBox.confirm(`删除映射 ${m.alias} → ${m.upstream_model}？`, '删除映射', { type: 'warning' })
  } catch { return }
  try {
    await apiFetch(`/api/v1/admin/model-mappings/${m.id}`, { method: 'DELETE' })
    void reloadMappings()
  } catch (e: any) { ElMessage.error(apiMessage(e, '删除失败')) }
}
const { data: allChannels } = useApiResource<{ items: { id: number; name: string }[] }>(
  () => apiFetch('/api/v1/admin/channels?limit=200'), { items: [] })
function scopeText(channel_id: number | null) { return channel_id == null ? '全局' : `渠道 ${channel_id}` }
</script>

<template>
  <div>
    <div class="head">
      <div><h1 class="pg-title">目录管理</h1><p class="pg-sub">对外模型、生效价格与渠道映射；写操作全部计入审计</p></div>
    </div>

    <el-card shadow="never">
      <el-tabs v-model="activeTab">
        <!-- 模型 -->
        <el-tab-pane label="模型" name="models">
          <div class="toolbar">
            <el-select v-model="modelProvider" placeholder="全部供应商" clearable style="width: 200px" @change="reloadModels">
              <el-option v-for="p in providers.items" :key="p.code" :label="p.code" :value="p.code" />
            </el-select>
            <div class="spacer" />
            <el-button type="primary" @click="openModelDialog()">新建模型</el-button>
          </div>
          <el-table v-loading="modelsLoading" :data="models.items" size="small" empty-text="暂无模型">
            <el-table-column prop="name" label="模型名" min-width="140"><template #default="{ row }"><code>{{ row.name }}</code></template></el-table-column>
            <el-table-column prop="display_name" label="展示名" min-width="130" />
            <el-table-column prop="provider_code" label="供应商" width="120" />
            <el-table-column label="上下文" width="110"><template #default="{ row }"><span class="num">{{ row.context_window?.toLocaleString() }}</span></template></el-table-column>
            <el-table-column label="能力" min-width="170"><template #default="{ row }"><span v-for="c in row.capabilities || []" :key="c" class="cf-tag cf-tag--muted">{{ capLabel[c] ?? c }}</span></template></el-table-column>
            <el-table-column label="计费" width="90"><template #default="{ row }">{{ row.billing_mode }}</template></el-table-column>
            <el-table-column label="状态" width="90"><template #default="{ row }"><span class="cf-tag" :class="row.status === 'active' ? 'cf-tag--success' : 'cf-tag--muted'">{{ row.status }}</span></template></el-table-column>
            <el-table-column label="操作" width="150" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" @click="openModelEdit(row as ModelItem)">编辑</el-button>
                <el-button link type="danger" @click="deleteModel(row as ModelItem)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
          <div v-if="modelsError" class="err">{{ modelsError }} <el-button link type="primary" @click="reloadModels">重试</el-button></div>
        </el-tab-pane>

        <!-- 定价 -->
        <el-tab-pane label="定价" name="prices">
          <div class="toolbar">
            <el-select v-model="priceModelId" placeholder="选择模型以查看定价" style="width: 260px" filterable @change="watchModelForPrice">
              <el-option v-for="m in models.items" :key="m.id" :label="m.name" :value="m.id" />
            </el-select>
            <el-button v-if="priceModelId" type="primary" :disabled="!priceModelId" @click="openPriceDialog()">新增价格</el-button>
          </div>
          <el-table v-loading="pricesLoading" :data="prices.items" size="small" empty-text="该模型暂无价格档——请新增当前生效价">
            <el-table-column label="生效日" width="110"><template #default="{ row }">{{ fmtPriceDate(row.effective_from) }}</template></el-table-column>
            <el-table-column label="输入 /1K" width="120"><template #default="{ row }"><span class="num">{{ row.input_price_per_1k }}</span></template></el-table-column>
            <el-table-column label="输出 /1K" width="120"><template #default="{ row }"><span class="num">{{ row.output_price_per_1k }}</span></template></el-table-column>
            <el-table-column label="按次" width="110"><template #default="{ row }"><span class="num">{{ row.per_request_price }}</span></template></el-table-column>
            <el-table-column label="失效日" min-width="110"><template #default="{ row }">{{ row.effective_to ? fmtPriceDate(row.effective_to) : '—' }}</template></el-table-column>
            <el-table-column label="操作" width="150" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" @click="openPriceEdit(row as PriceItem)">编辑</el-button>
                <el-button link type="danger" @click="deletePrice(row as PriceItem)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
          <div v-if="pricesError" class="err">{{ pricesError }} <el-button link type="primary" @click="reloadPrices">重试</el-button></div>
        </el-tab-pane>

        <!-- 映射 -->
        <el-tab-pane label="映射" name="mappings">
          <div class="toolbar">
            <el-input v-model="aliasFilter" placeholder="按 alias 筛选" clearable style="width: 200px" @keyup.enter="reloadMappings" @clear="reloadMappings" />
            <el-button @click="reloadMappings">查询</el-button>
            <div class="spacer" />
            <el-button type="primary" @click="openMappingDialog">新增映射</el-button>
          </div>
          <el-table v-loading="mappingsLoading" :data="mappings.items" size="small" empty-text="暂无映射（未映射的模型将按同名透传）">
            <el-table-column label="作用域" width="110"><template #default="{ row }">{{ scopeText(row.channel_id) }}</template></el-table-column>
            <el-table-column prop="alias" label="alias" min-width="150"><template #default="{ row }"><code>{{ row.alias }}</code></template></el-table-column>
            <el-table-column prop="upstream_model" label="上游模型" min-width="160"><template #default="{ row }"><code>{{ row.upstream_model }}</code></template></el-table-column>
            <el-table-column prop="priority" label="优先级" width="90"><template #default="{ row }"><span class="num">{{ row.priority }}</span></template></el-table-column>
            <el-table-column label="操作" width="90" fixed="right">
              <template #default="{ row }"><el-button link type="danger" @click="deleteMapping(row as MappingItem)">删除</el-button></template>
            </el-table-column>
          </el-table>
          <div v-if="mappingsError" class="err">{{ mappingsError }} <el-button link type="primary" @click="reloadMappings">重试</el-button></div>
        </el-tab-pane>
      </el-tabs>
    </el-card>

    <!-- 模型新建/编辑 -->
    <el-dialog v-model="modelDialogOpen" :title="modelEditing ? '编辑模型' : '新建模型'" width="560px">
      <el-form label-position="top">
        <div class="f-grid">
          <el-form-item label="模型名（对外标识，唯一）"><el-input v-model="modelForm.name" /></el-form-item>
          <el-form-item label="供应商"><el-select v-model="modelForm.provider_code" style="width: 100%"><el-option v-for="p in providers.items" :key="p.code" :label="p.code" :value="p.code" /></el-select></el-form-item>
        </div>
        <el-form-item label="展示名"><el-input v-model="modelForm.display_name" /></el-form-item>
        <div class="f-grid">
          <el-form-item label="上下文窗口"><el-input-number v-model="modelForm.context_window" :min="0" style="width: 100%" /></el-form-item>
          <el-form-item label="最大输出"><el-input-number v-model="modelForm.max_output_tokens" :min="0" style="width: 100%" /></el-form-item>
        </div>
        <el-form-item label="能力">
          <el-checkbox-group v-model="modelForm.capabilities">
            <el-checkbox v-for="c in CAPS" :key="c" :value="c">{{ capLabel[c] }}</el-checkbox>
          </el-checkbox-group>
        </el-form-item>
        <div class="f-grid">
          <el-form-item label="计费模式">
            <el-select v-model="modelForm.billing_mode" style="width: 100%"><el-option v-for="m in BMODES" :key="m" :label="m" :value="m" /></el-select>
          </el-form-item>
          <el-form-item label="状态">
            <el-select v-model="modelForm.status" style="width: 100%">
              <el-option label="active" value="active" /><el-option label="deprecated" value="deprecated" /><el-option label="hidden" value="hidden" />
            </el-select>
          </el-form-item>
        </div>
        <el-form-item label="降级链 fallbacks（每行一个模型名）"><el-input v-model="modelForm.fallbacks" type="textarea" :rows="2" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="modelDialogOpen = false">取消</el-button>
        <el-button type="primary" :loading="modelSaving" @click="saveModel">保存</el-button>
      </template>
    </el-dialog>

    <!-- 价格新建/编辑 -->
    <el-dialog v-model="priceFormOpen" :title="priceEditing ? '编辑价格' : '新增价格'" width="520px">
      <el-form label-position="top">
        <el-alert v-if="priceEditing" type="info" :closable="false" title="编辑保持 cache 单价不变（响应不回显现值）；如需调整请删除后按新价重建" style="margin-bottom: 10px" />
        <div class="f-grid">
          <el-form-item label="生效日（YYYY-MM-DD）">
            <el-date-picker v-model="priceForm.effective_from" type="date" value-format="YYYY-MM-DD" style="width: 100%" :disabled="priceEditing" />
          </el-form-item>
          <el-form-item label="失效日（可选）">
            <el-date-picker v-model="priceForm.effective_to" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
          </el-form-item>
        </div>
        <div class="f-grid">
          <el-form-item label="输入价 /1K"><el-input v-model="priceForm.input" placeholder="0.01" /></el-form-item>
          <el-form-item label="输出价 /1K"><el-input v-model="priceForm.output" placeholder="0.03" /></el-form-item>
          <el-form-item label="按次费用"><el-input v-model="priceForm.per_request" placeholder="可选" /></el-form-item>
          <el-form-item v-if="!priceEditing" label="缓存读 /1K"><el-input v-model="priceForm.cache_read" placeholder="可选" /></el-form-item>
          <el-form-item v-if="!priceEditing" label="缓存写 /1K"><el-input v-model="priceForm.cache_write" placeholder="可选" /></el-form-item>
          <el-form-item label="币种"><el-select v-model="priceForm.currency" style="width: 100%"><el-option label="USD" value="USD" /></el-select></el-form-item>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="priceFormOpen = false">取消</el-button>
        <el-button type="primary" :loading="priceSaving" @click="savePrice">保存</el-button>
      </template>
    </el-dialog>

    <!-- 映射新建 -->
    <el-dialog v-model="mappingOpen" title="新增映射" width="480px">
      <el-form label-position="top">
        <div class="f-grid">
          <el-form-item label="alias（对外模型名/通配）"><el-input v-model="mappingForm.alias" placeholder="gpt-4o 或 gpt-4*" /></el-form-item>
          <el-form-item label="上游模型名"><el-input v-model="mappingForm.upstream_model" placeholder="claude-3-5-sonnet" /></el-form-item>
        </div>
        <div class="f-grid">
          <el-form-item label="作用域">
            <el-radio-group v-model="mappingForm.scope">
              <el-radio-button value="global">全局</el-radio-button>
              <el-radio-button value="channel">指定渠道</el-radio-button>
            </el-radio-group>
          </el-form-item>
          <el-form-item v-if="mappingForm.scope === 'channel'" label="渠道">
            <el-select v-model="mappingForm.channel_id" style="width: 100%">
              <el-option v-for="c in allChannels.items" :key="c.id" :label="`#${c.id} ${c.name}`" :value="c.id" />
            </el-select>
          </el-form-item>
        </div>
        <el-form-item label="优先级"><el-input-number v-model="mappingForm.priority" :min="0" style="width: 100%" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="mappingOpen = false">取消</el-button>
        <el-button type="primary" :loading="mappingSaving" @click="saveMapping">创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.pg-title { font-size: 22px; font-weight: 700; }
.pg-sub { font-size: 13px; color: var(--cf-text-secondary); }
.head { margin-bottom: 16px; }
.toolbar { display: flex; align-items: center; gap: 10px; margin-bottom: 14px; flex-wrap: wrap; }
.spacer { flex: 1; }
.f-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0 12px; }
.err { margin-top: 10px; font-size: 13px; color: var(--cf-semantic-danger); }
.cf-tag + .cf-tag { margin-left: 4px; }

@media (width < 640px) {
  .f-grid { grid-template-columns: 1fr; }
}
</style>
