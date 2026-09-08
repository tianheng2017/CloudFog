<script setup lang="ts">
// API 密钥管理（07 §3.1 /me/keys）：明文仅创建时展示一次（08 §3.1）
// 注意契约细节：创建请求白名单键为下划线 ip_whitelist/model_whitelist；
// 编辑 PATCH 键为**无下划线** ipwhitelist/modelwhitelist 且全量替换（空数组=清空）。
definePageMeta({ layout: 'console' })
interface KeyItem {
  id: number
  name: string
  group_id: number | null
  key_prefix: string
  status: string
  expires_at: string | null
  ip_whitelist: string[] | null
  model_whitelist: string[] | null
  created_at?: string
}
interface GroupItem { id: number; name: string; status: string; is_default: boolean }

const { data: keys, loading, error, reload: load } = useApiResource<{ items: KeyItem[] }>(
  () => apiFetch('/api/v1/me/keys'), { items: [] })
const { data: groups } = useApiResource<{ items: GroupItem[] }>(
  () => apiFetch('/api/v1/me/groups'), { items: [] })
function groupName(id: number | null) {
  const g = groups.value.items.find(x => x.id === id)
  return g ? g.name + (g.is_default ? '（默认）' : '') : (id ? String(id) : '—')
}

const statusMap: Record<string, { label: string; cls: string }> = {
  active: { label: '启用', cls: 'cf-tag--success' },
  disabled: { label: '已禁用', cls: 'cf-tag--danger' },
  expired: { label: '已过期', cls: 'cf-tag--warning' },
}
function statusOf(s: string) { return statusMap[s] ?? { label: s, cls: 'cf-tag--muted' } }

// ── 创建 ──
const createOpen = ref(false)
const creating = ref(false)
const createForm = reactive({
  name: '',
  group_id: undefined as number | undefined,
  expires_at: null as number | null, // EP datetime picker value-format=x → ms
  ip_whitelist: '',
  model_whitelist: '',
})
const token = ref('')
const tokenOpen = ref(false)

// 文本域（每行/逗号一个）→ 数组；空 → 不传（创建）/传 []（编辑清空见调用处）
function toList(s: string): string[] {
  return s.split(/[\n,]/).map(x => x.trim()).filter(Boolean)
}
function openCreate() {
  Object.assign(createForm, { name: '', group_id: undefined, expires_at: null, ip_whitelist: '', model_whitelist: '' })
  createOpen.value = true
}

async function create() {
  creating.value = true
  try {
    const body: Record<string, any> = { name: createForm.name.trim() || 'default' }
    const ip = toList(createForm.ip_whitelist)
    const mw = toList(createForm.model_whitelist)
    if (createForm.group_id != null) body.group_id = createForm.group_id
    if (createForm.expires_at) body.expires_at = new Date(createForm.expires_at).toISOString()
    if (ip.length) body.ip_whitelist = ip
    if (mw.length) body.model_whitelist = mw
    const r = await apiFetch<{ id: number; token: string }>('/api/v1/me/keys', { method: 'POST', body })
    createOpen.value = false
    token.value = r.token
    tokenOpen.value = true
    await load()
  } catch (e: any) {
    ElMessage.error(apiMessage(e, '创建失败'))
  } finally { creating.value = false }
}

// ── 编辑（PATCH：name/status/ipwhitelist/modelwhitelist，无下划线、全量替换）──
const editOpen = ref(false)
const savingEdit = ref(false)
const editForm = reactive({
  id: 0,
  name: '',
  status: 'active',
  ip_whitelist: '',
  model_whitelist: '',
})
function openEdit(k: KeyItem) {
  Object.assign(editForm, {
    id: k.id,
    name: k.name,
    status: k.status === 'active' ? 'active' : 'disabled',
    ip_whitelist: (k.ip_whitelist || []).join('\n'),
    model_whitelist: (k.model_whitelist || []).join('\n'),
  })
  editOpen.value = true
}
async function saveEdit() {
  savingEdit.value = true
  try {
    await apiFetch(`/api/v1/me/keys/${editForm.id}`, {
      method: 'PATCH',
      body: {
        name: editForm.name.trim(),
        status: editForm.status,
        ipwhitelist: toList(editForm.ip_whitelist),
        modelwhitelist: toList(editForm.model_whitelist),
      },
    })
    editOpen.value = false
    ElMessage.success('已更新')
    await load()
  } catch (e: any) {
    ElMessage.error(apiMessage(e, '更新失败'))
  } finally { savingEdit.value = false }
}

async function setStatus(k: KeyItem, status: 'active' | 'disabled') {
  try {
    await apiFetch(`/api/v1/me/keys/${k.id}`, { method: 'PATCH', body: { status } })
    await load()
  } catch (e: any) { ElMessage.error(apiMessage(e, '操作失败')) }
}

async function remove(k: KeyItem) {
  try {
    await ElMessageBox.confirm(`删除密钥 ${k.name} 后将立即失效，确定？`, '删除密钥', { type: 'warning' })
  } catch { return }
  try {
    await apiFetch(`/api/v1/me/keys/${k.id}`, { method: 'DELETE' })
    await load()
  } catch (e: any) { ElMessage.error(apiMessage(e, '删除失败')) }
}

async function copyText(s: string) {
  try {
    if (!navigator.clipboard) throw new Error('unsupported')
    await navigator.clipboard.writeText(s)
    ElMessage.success('已复制，请妥善保存（不再显示）')
  } catch {
    ElMessage.warning('自动复制失败，请手动选中复制')
  }
}

function fmtTime(t: string | null) { return t ? formatDateTime(t) : '—' }
function fmtList(a: string[] | null, cap = 2) {
  const arr = a || []
  if (!arr.length) return '—'
  const show = arr.slice(0, cap)
  return show.join(', ') + (arr.length > cap ? ` 等 ${arr.length} 条` : '')
}
</script>

<template>
  <div>
    <div class="head">
      <div><h1 class="pg-title">API 密钥</h1><p class="pg-sub">密钥仅显示前缀；明文在创建时展示一次，请立即保存</p></div>
      <el-button type="primary" @click="openCreate">创建密钥</el-button>
    </div>

    <div v-if="error" class="err">{{ error }} <el-button link type="primary" @click="load">重试</el-button></div>

    <el-table v-loading="loading" :data="keys.items" style="width: 100%" empty-text="还没有密钥，点击右上角创建">
      <el-table-column prop="name" label="名称" min-width="130" />
      <el-table-column label="前缀" min-width="130">
        <template #default="{ row }"><code class="num">{{ row.key_prefix }}****</code></template>
      </el-table-column>
      <el-table-column label="分组" min-width="120"><template #default="{ row }">{{ groupName(row.group_id) }}</template></el-table-column>
      <el-table-column label="状态" width="95">
        <template #default="{ row }"><span class="cf-tag" :class="statusOf(row.status).cls">{{ statusOf(row.status).label }}</span></template>
      </el-table-column>
      <el-table-column label="有效期至" min-width="160"><template #default="{ row }">{{ fmtTime(row.expires_at) }}</template></el-table-column>
      <el-table-column label="IP 白名单" min-width="150">
        <template #default="{ row }">
          <el-tooltip :content="(row.ip_whitelist || []).join(', ') || '未限制'" placement="top">
            <span>{{ fmtList(row.ip_whitelist) }}</span>
          </el-tooltip>
        </template>
      </el-table-column>
      <el-table-column label="创建时间" min-width="160"><template #default="{ row }">{{ formatDateTime(row.created_at) }}</template></el-table-column>
      <el-table-column label="操作" width="210" fixed="right">
        <template #default="{ row }">
          <el-button v-if="row.status === 'active'" link type="warning" @click="setStatus(row as KeyItem, 'disabled')">禁用</el-button>
          <el-button v-else link type="success" @click="setStatus(row as KeyItem, 'active')">启用</el-button>
          <el-button link type="primary" @click="openEdit(row as KeyItem)">编辑</el-button>
          <el-button link type="danger" @click="remove(row as KeyItem)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 创建 -->
    <el-dialog v-model="createOpen" title="创建密钥" width="480px">
      <el-form label-position="top">
        <el-form-item label="名称（可选，默认 default）">
          <el-input v-model="createForm.name" placeholder="例如：生产环境" maxlength="100" />
        </el-form-item>
        <el-form-item label="分组">
          <el-select v-model="createForm.group_id" clearable placeholder="默认分组（不选=使用默认分组）" style="width: 100%">
            <el-option v-for="g in groups.items.filter(x => x.status === 'active')" :key="g.id" :label="g.name + (g.is_default ? '（默认）' : '')" :value="g.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="有效期至（可选，最晚 2 年）">
          <el-date-picker v-model="createForm.expires_at" type="datetime" value-format="x" placeholder="选择过期时间" style="width: 100%" />
        </el-form-item>
        <el-form-item label="IP 白名单（每行一条 IP 或 CIDR，可选）">
          <el-input v-model="createForm.ip_whitelist" type="textarea" :rows="3" placeholder="1.2.3.4&#10;10.0.0.0/8" />
        </el-form-item>
        <el-form-item label="模型白名单（每行一个模型名，可选）">
          <el-input v-model="createForm.model_whitelist" type="textarea" :rows="2" placeholder="gpt-4o&#10;gpt-4o-mini" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createOpen = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="create">创建</el-button>
      </template>
    </el-dialog>

    <!-- 编辑 -->
    <el-dialog v-model="editOpen" title="编辑密钥" width="480px">
      <el-form label-position="top">
        <el-form-item label="名称">
          <el-input v-model="editForm.name" maxlength="100" />
        </el-form-item>
        <el-form-item label="状态">
          <el-radio-group v-model="editForm.status">
            <el-radio-button value="active">启用</el-radio-button>
            <el-radio-button value="disabled">禁用</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="IP 白名单（整段替换；清空即移除限制）">
          <el-input v-model="editForm.ip_whitelist" type="textarea" :rows="3" placeholder="每行一条 IP 或 CIDR" />
        </el-form-item>
        <el-form-item label="模型白名单（整段替换；清空即不限制）">
          <el-input v-model="editForm.model_whitelist" type="textarea" :rows="2" placeholder="每行一个模型名" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editOpen = false">取消</el-button>
        <el-button type="primary" :loading="savingEdit" @click="saveEdit">保存</el-button>
      </template>
    </el-dialog>

    <!-- 创建成功：明文一次 -->
    <el-dialog v-model="tokenOpen" title="密钥已创建" width="480px" :close-on-click-modal="false">
      <el-alert type="warning" title="请立即复制保存——此明文仅显示这一次" :closable="false" style="margin-bottom: 12px" />
      <el-input v-model="token" type="textarea" :rows="3" readonly>
        <template #append><el-button @click="copyText(token)">复制</el-button></template>
      </el-input>
      <template #footer><el-button type="primary" @click="tokenOpen = false">我已保存</el-button></template>
    </el-dialog>
  </div>
</template>

<style scoped>
.head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 20px; }
.pg-title { font-size: 22px; font-weight: 700; }
.pg-sub { margin-top: 2px; font-size: 13px; color: var(--cf-text-secondary); }
.err { margin-bottom: 12px; font-size: 13px; color: var(--cf-semantic-danger); }

@media (width < 640px) {
  .head { flex-direction: column; align-items: stretch; }
}
</style>
