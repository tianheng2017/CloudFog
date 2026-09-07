<script setup lang="ts">
// API 密钥管理（07 §3.1 /me/keys）：明文仅创建时展示一次（08 §3.1）
definePageMeta({ layout: 'console' })
interface KeyItem {
  id: number
  name: string
  group_id: number | null
  key_prefix: string
  status: string
  created_at?: string
}
const items = ref<KeyItem[]>([])
const loading = ref(false)
const createOpen = ref(false)
const creating = ref(false)
const newName = ref('')
const token = ref('')
const tokenOpen = ref(false)

// 状态中文展示（页面状态一律中文）
const statusMap: Record<string, { label: string; cls: string }> = {
  active: { label: '启用', cls: 'cf-tag--success' },
  disabled: { label: '已禁用', cls: 'cf-tag--muted' },
  expired: { label: '已过期', cls: 'cf-tag--warning' },
}
function statusOf(s: string) { return statusMap[s] ?? { label: s, cls: 'cf-tag--muted' } }

async function load() {
  loading.value = true
  try {
    const r = await $fetch<{ items: KeyItem[] }>('/api/v1/me/keys', { credentials: 'include' })
    items.value = r.items || []
  } catch { /* 鉴权失败由 auth.global 兜底跳登录；网络错误静默（页面空态） */ } finally { loading.value = false }
}
onMounted(load)

async function create() {
  creating.value = true
  try {
    const r = await $fetch<{ id: number; token: string }>('/api/v1/me/keys', {
      method: 'POST', credentials: 'include',
      body: { name: newName.value || 'default' },
    })
    createOpen.value = false
    token.value = r.token
    tokenOpen.value = true
    await load()
  } catch (e: any) {
    ElMessage.error(e?.data?.error?.message || '创建失败')
  } finally { creating.value = false }
}

async function setStatus(k: KeyItem, status: 'active' | 'disabled') {
  try {
    await $fetch(`/api/v1/me/keys/${k.id}`, { method: 'PATCH', credentials: 'include', body: { status } })
    await load()
  } catch (e: any) { ElMessage.error(e?.data?.error?.message || '操作失败') }
}

async function remove(k: KeyItem) {
  try {
    await ElMessageBox.confirm(`删除密钥 ${k.name} 后将立即失效，确定？`, '删除密钥', { type: 'warning' })
  } catch { return }
  try {
    await $fetch(`/api/v1/me/keys/${k.id}`, { method: 'DELETE', credentials: 'include' })
    await load()
  } catch (e: any) { ElMessage.error(e?.data?.error?.message || '删除失败') }
}

function copyToken() {
  if (navigator.clipboard) navigator.clipboard.writeText(token.value)
  ElMessage.success('已复制，请妥善保存（不再显示）')
}
</script>

<template>
  <div>
    <div class="head">
      <div><h1 class="pg-title">API 密钥</h1><p class="pg-sub">密钥仅显示前缀；明文在创建时展示一次，请立即保存</p></div>
      <el-button type="primary" @click="createOpen = true">创建密钥</el-button>
    </div>

    <el-table v-loading="loading" :data="items" style="width: 100%" empty-text="还没有密钥，点击右上角创建">
      <el-table-column prop="name" label="名称" min-width="140" />
      <el-table-column label="前缀" min-width="150">
        <template #default="{ row }">
          <code class="num">{{ row.key_prefix }}****</code>
        </template>
      </el-table-column>
      <el-table-column prop="group_id" label="分组 ID" width="100"><template #default="{ row }">{{ row.group_id ?? '—' }}</template></el-table-column>
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <span class="cf-tag" :class="statusOf(row.status).cls">{{ statusOf(row.status).label }}</span>
        </template>
      </el-table-column>
      <el-table-column label="创建时间" min-width="170"><template #default="{ row }">{{ formatDateTime(row.created_at) }}</template></el-table-column>
      <el-table-column label="操作" width="200" fixed="right">
        <template #default="{ row }">
          <el-button v-if="row.status === 'active'" link type="warning" @click="setStatus(row as KeyItem, 'disabled')">禁用</el-button>
          <el-button v-else link type="success" @click="setStatus(row as KeyItem, 'active')">启用</el-button>
          <el-button link type="danger" @click="remove(row as KeyItem)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="createOpen" title="创建密钥" width="420px">
      <el-form label-position="top">
        <el-form-item label="名称（可选，默认 default）">
          <el-input v-model="newName" placeholder="例如：生产环境" maxlength="100" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createOpen = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="create">创建</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="tokenOpen" title="密钥已创建" width="480px" :close-on-click-modal="false">
      <el-alert type="warning" title="请立即复制保存——此明文仅显示这一次" :closable="false" style="margin-bottom: 12px" />
      <el-input v-model="token" type="textarea" :rows="3" readonly>
        <template #append><el-button @click="copyToken">复制</el-button></template>
      </el-input>
      <template #footer><el-button type="primary" @click="tokenOpen = false">我已保存</el-button></template>
    </el-dialog>
  </div>
</template>

<style scoped>
.head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 20px; }
.pg-title { font-size: 22px; font-weight: 700; }
.pg-sub { margin-top: 2px; font-size: 13px; color: var(--cf-text-secondary); }
</style>
