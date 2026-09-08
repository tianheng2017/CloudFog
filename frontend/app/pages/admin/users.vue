<script setup lang="ts">
// 管理 · 用户（07 §4.1）：建用户 / 启用禁用 / 角色 / 调账 / 分组 / 详情
// 分页为 offset/limit（默认 20，上限 200）；金额一律字符串。
definePageMeta({ layout: 'admin' })
const store = useUserStore()
const isSuper = computed(() => store.me?.role === 'super_admin')

interface UserItem {
  id: number; email: string; username: string; role: string; status: string; risk_level: string; created_at: string
}
interface GroupItem { id: number; name: string; status: string; rate_multiplier?: string }
interface KeyLite { id: number; name: string; key_prefix: string; status: string; expires_at: string | null }

// ── 列表（offset/limit）──
const offset = ref(0)
const limit = ref(20)
const keyword = ref('')
const statusFilter = ref('')
const roleFilter = ref('')
const { data: users, loading, error, reload } = useApiResource<{ items: UserItem[]; total: number }>(
  () => {
    const q = new URLSearchParams({ offset: String(offset.value), limit: String(limit.value) })
    if (keyword.value.trim()) q.set('keyword', keyword.value.trim())
    if (statusFilter.value) q.set('status', statusFilter.value)
    if (roleFilter.value) q.set('role', roleFilter.value)
    return apiFetch(`/api/v1/admin/users?${q}`)
  },
  { items: [], total: 0 })
function onSearch() { offset.value = 0; void reload() }
function onPage(p: number) { offset.value = (p - 1) * limit.value; void reload() }
function onSize(s: number) { limit.value = s; offset.value = 0; void reload() }

const { data: groups } = useApiResource<{ items: GroupItem[] }>(
  () => apiFetch('/api/v1/admin/groups'), { items: [] })
function groupOpts(activeOnly = false) {
  const all = groups.value.items
  return activeOnly ? all.filter(g => g.status === 'active') : all
}

const roleTagMap: Record<string, string> = { super_admin: 'danger', admin: 'warning', user: 'muted' }
const roleLabelMap: Record<string, string> = { super_admin: '超管', admin: '管理员', user: '用户' }
const statusTagMap: Record<string, string> = { active: 'success', disabled: 'danger', pending: 'warning' }
const statusLabelMap: Record<string, string> = { active: '启用', disabled: '已禁用', pending: '待激活' }

// ── 新建用户 ──
const createOpen = ref(false)
const creating = ref(false)
const createForm = reactive({ username: '', email: '', password: '', role: 'user', default_group_id: undefined as number | undefined })
function openCreate() {
  Object.assign(createForm, { username: '', email: '', password: '', role: 'user', default_group_id: undefined })
  createOpen.value = true
}
async function createUser() {
  const f = createForm
  if (!f.username.trim() || !f.email.trim() || f.password.length < 8) {
    ElMessage.warning('用户名/邮箱必填，密码至少 8 位'); return
  }
  creating.value = true
  try {
    await apiFetch('/api/v1/admin/users', {
      method: 'POST',
      body: { username: f.username, email: f.email, password: f.password, role: f.role, ...(f.default_group_id ? { default_group_id: f.default_group_id } : {}) },
    })
    createOpen.value = false
    ElMessage.success('用户已创建')
    void reload()
  } catch (e: any) { ElMessage.error(apiMessage(e, '创建失败')) } finally { creating.value = false }
}

// ── 禁用（必填 reason）──
const disableOpen = ref(false)
const disabling = ref(false)
const disableTarget = ref<UserItem | null>(null)
const disableReason = ref('')
function openDisable(u: UserItem) {
  disableTarget.value = u
  disableReason.value = ''
  disableOpen.value = true
}
async function doDisable() {
  if (!disableTarget.value) return
  if (!disableReason.value.trim()) { ElMessage.warning('请填写禁用原因（≤500 字）'); return }
  disabling.value = true
  try {
    await apiFetch(`/api/v1/admin/users/${disableTarget.value.id}/disable`, { method: 'POST', body: { reason: disableReason.value.trim() } })
    disableOpen.value = false
    ElMessage.success('已禁用')
    void reload()
  } catch (e: any) { ElMessage.error(apiMessage(e, '操作失败')) } finally { disabling.value = false }
}
async function doEnable(u: UserItem) {
  try {
    await apiFetch(`/api/v1/admin/users/${u.id}/enable`, { method: 'POST' })
    ElMessage.success('已启用')
    void reload()
  } catch (e: any) { ElMessage.error(apiMessage(e, '操作失败')) }
}

// ── 角色变更（仅 super_admin，不能改自己）──
const roleOpen = ref(false)
const roleTarget = ref<UserItem | null>(null)
const roleVal = ref('user')
const roleSaving = ref(false)
function openRole(u: UserItem) { roleTarget.value = u; roleVal.value = u.role; roleOpen.value = true }
async function saveRole() {
  if (!roleTarget.value) return
  roleSaving.value = true
  try {
    await apiFetch(`/api/v1/admin/users/${roleTarget.value.id}/role`, { method: 'PATCH', body: { role: roleVal.value } })
    roleOpen.value = false
    ElMessage.success('角色已更新')
    void reload()
  } catch (e: any) { ElMessage.error(apiMessage(e, '更新失败')) } finally { roleSaving.value = false }
}

// ── 调账（手动 ±，|amt|>500 需超管）──
const adjustOpen = ref(false)
const adjustTarget = ref<UserItem | null>(null)
const adjustForm = reactive({ amount: '', reason: '' })
const adjusting = ref(false)
function openAdjust(u: UserItem) {
  adjustTarget.value = u
  Object.assign(adjustForm, { amount: '', reason: '' })
  adjustOpen.value = true
}
async function doAdjust() {
  const amt = adjustForm.amount.trim()
  if (!/^-?\d+(\.\d{1,2})?$/.test(amt) || amt === '0') { ElMessage.warning('金额须为非零十进制（负数为扣减）'); return }
  if (!adjustForm.reason.trim()) { ElMessage.warning('请填写调账原因'); return }
  adjusting.value = true
  try {
    await apiFetch(`/api/v1/admin/users/${adjustTarget.value!.id}/balance`, {
      method: 'POST',
      body: { amount: amt, reason: adjustForm.reason.trim() },
    })
    adjustOpen.value = false
    ElMessage.success('调账完成')
    void reload()
  } catch (e: any) { ElMessage.error(apiMessage(e, '调账失败')) } finally { adjusting.value = false }
}

// ── 详情 drawer（余额 + 密钥概览）──
const detailOpen = ref(false)
const detail = ref<Record<string, any> | null>(null)
const detailKeys = ref<KeyLite[]>([])
const detailLoading = ref(false)
async function openDetail(u: UserItem) {
  detailOpen.value = true
  detailLoading.value = true
  detail.value = null
  detailKeys.value = []
  try {
    const [d, k] = await Promise.all([
      apiFetch(`/api/v1/admin/users/${u.id}`),
      apiFetch<{ items: KeyLite[] }>(`/api/v1/admin/users/${u.id}/keys`),
    ])
    detail.value = d
    detailKeys.value = k.items || []
  } catch (e: any) { ElMessage.error(apiMessage(e, '加载详情失败')) } finally { detailLoading.value = false }
}

// ── 分组绑定（PUT group_ids 全量替换）──
const groupsOpen = ref(false)
const groupsTarget = ref<UserItem | null>(null)
const groupIds = ref<number[]>([])
const groupsSaving = ref(false)
async function openGroups(u: UserItem) {
  groupsTarget.value = u
  groupIds.value = []
  groupsOpen.value = true
  try {
    const r = await apiFetch<{ group_ids: number[] }>(`/api/v1/admin/users/${u.id}/groups`)
    groupIds.value = r.group_ids || []
  } catch { /* 关闭时给出错 */ }
}
async function saveGroups() {
  if (!groupsTarget.value) return
  groupsSaving.value = true
  try {
    await apiFetch(`/api/v1/admin/users/${groupsTarget.value.id}/groups`, { method: 'PUT', body: { group_ids: groupIds.value } })
    groupsOpen.value = false
    ElMessage.success('分组已更新')
  } catch (e: any) { ElMessage.error(apiMessage(e, '更新失败')) } finally { groupsSaving.value = false }
}

function fmtT(t: string | null | undefined) { return t ? formatDateTime(t) : '—' }
</script>

<template>
  <div>
    <div class="head">
      <div><h1 class="pg-title">用户管理</h1><p class="pg-sub">创建 / 启用禁用 / 角色 / 调账；管理写操作均计入审计</p></div>
      <el-button type="primary" @click="openCreate">新建用户</el-button>
    </div>

    <el-card shadow="never">
      <div class="toolbar">
        <el-input v-model="keyword" placeholder="用户名/邮箱" clearable style="width: 200px" @keyup.enter="onSearch" @clear="onSearch" />
        <el-select v-model="statusFilter" placeholder="状态" clearable style="width: 130px" @change="onSearch">
          <el-option label="启用" value="active" /><el-option label="已禁用" value="disabled" />
        </el-select>
        <el-select v-model="roleFilter" placeholder="角色" clearable style="width: 140px" @change="onSearch">
          <el-option label="用户" value="user" /><el-option label="管理员" value="admin" /><el-option label="超管" value="super_admin" />
        </el-select>
        <el-button @click="onSearch">查询</el-button>
        <el-button v-if="error" link type="primary" @click="reload">重试</el-button>
      </div>

      <el-table v-loading="loading" :data="users.items" style="width: 100%" empty-text="暂无用户">
        <el-table-column prop="id" label="ID" width="70"><template #default="{ row }"><span class="num">{{ row.id }}</span></template></el-table-column>
        <el-table-column prop="username" label="用户名" min-width="130" />
        <el-table-column prop="email" label="邮箱" min-width="170"><template #default="{ row }">{{ row.email || '—' }}</template></el-table-column>
        <el-table-column label="角色" width="110"><template #default="{ row }"><span class="cf-tag" :class="'cf-tag--' + roleTagMap[row.role]">{{ roleLabelMap[row.role] }}</span></template></el-table-column>
        <el-table-column label="状态" width="95"><template #default="{ row }"><span class="cf-tag" :class="'cf-tag--' + statusTagMap[row.status]">{{ statusLabelMap[row.status] }}</span></template></el-table-column>
        <el-table-column prop="risk_level" label="风险" width="80" />
        <el-table-column label="创建时间" min-width="160"><template #default="{ row }">{{ fmtT(row.created_at) }}</template></el-table-column>
        <el-table-column label="操作" width="340" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetail(row as UserItem)">详情</el-button>
            <el-button link type="success" @click="openGroups(row as UserItem)">分组</el-button>
            <template v-if="row.id !== store.me?.id">
              <el-button v-if="row.status === 'active'" link type="warning" @click="openDisable(row as UserItem)">禁用</el-button>
              <el-button v-else link type="success" @click="doEnable(row as UserItem)">启用</el-button>
              <el-button v-if="isSuper" link type="primary" @click="openRole(row as UserItem)">角色</el-button>
              <el-button link type="warning" @click="openAdjust(row as UserItem)">调账</el-button>
            </template>
            <span v-else class="self-tip">当前账号</span>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination
        v-if="users.total > limit"
        class="page-bar"
        layout="total, prev, pager, next, sizes"
        :total="users.total"
        :page-size="limit"
        :current-page="offset / limit + 1"
        :page-sizes="[20, 50, 100, 200]"
        @current-change="onPage"
        @size-change="onSize"
      />
    </el-card>

    <!-- 新建 -->
    <el-dialog v-model="createOpen" title="新建用户" width="460px">
      <el-form label-position="top">
        <el-form-item label="用户名（唯一，自动小写）"><el-input v-model="createForm.username" maxlength="64" /></el-form-item>
        <el-form-item label="邮箱（唯一，自动小写）"><el-input v-model="createForm.email" maxlength="128" /></el-form-item>
        <el-form-item label="初始密码（≥8 位）"><el-input v-model="createForm.password" type="password" show-password /></el-form-item>
        <el-form-item label="角色">
          <el-select v-model="createForm.role" style="width: 100%">
            <el-option label="用户" value="user" />
            <el-option v-if="isSuper" label="管理员" value="admin" />
            <el-option v-if="isSuper" label="超管" value="super_admin" />
          </el-select>
        </el-form-item>
        <el-form-item label="默认分组（可选）">
          <el-select v-model="createForm.default_group_id" clearable placeholder="不选则不绑定默认分组" style="width: 100%">
            <el-option v-for="g in groupOpts(true)" :key="g.id" :label="g.name" :value="g.id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createOpen = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="createUser">创建</el-button>
      </template>
    </el-dialog>

    <!-- 禁用 -->
    <el-dialog v-model="disableOpen" title="禁用用户" width="440px">
      <p class="d-tip">将禁用 <b>{{ disableTarget?.username }}</b>（{{ disableTarget?.email }}）。禁用后其密钥/会话即时失效。</p>
      <el-input v-model="disableReason" type="textarea" :rows="3" maxlength="500" show-word-limit placeholder="必填：禁用原因（将记录到审计日志）" />
      <template #footer>
        <el-button @click="disableOpen = false">取消</el-button>
        <el-button type="danger" :loading="disabling" @click="doDisable">确认禁用</el-button>
      </template>
    </el-dialog>

    <!-- 角色 -->
    <el-dialog v-model="roleOpen" title="变更角色" width="400px">
      <p class="d-tip">变更 <b>{{ roleTarget?.username }}</b> 的角色（仅超管可操作，审计记录）。</p>
      <el-select v-model="roleVal" style="width: 100%">
        <el-option label="用户" value="user" /><el-option label="管理员" value="admin" /><el-option label="超管" value="super_admin" />
      </el-select>
      <template #footer>
        <el-button @click="roleOpen = false">取消</el-button>
        <el-button type="primary" :loading="roleSaving" @click="saveRole">保存</el-button>
      </template>
    </el-dialog>

    <!-- 调账 -->
    <el-dialog v-model="adjustOpen" title="余额调账" width="440px">
      <p class="d-tip">目标：<b>{{ adjustTarget?.username }}</b>（当前不可致负；<b>|金额| &gt; 500 需超管</b>）</p>
      <el-form label-position="top">
        <el-form-item label="金额（USD，正=充值，负=扣减）">
          <el-input v-model="adjustForm.amount" placeholder="例如 100 或 -50" />
        </el-form-item>
        <el-form-item label="原因（必填，≤500 字）">
          <el-input v-model="adjustForm.reason" type="textarea" :rows="2" maxlength="500" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="adjustOpen = false">取消</el-button>
        <el-button type="warning" :loading="adjusting" @click="doAdjust">确认调账</el-button>
      </template>
    </el-dialog>

    <!-- 分组绑定 -->
    <el-dialog v-model="groupsOpen" :title="`分组绑定：${groupsTarget?.username}`" width="440px">
      <p class="d-tip">全量替换该用户可见分组（多选）。</p>
      <el-select v-model="groupIds" multiple style="width: 100%" placeholder="选择分组">
        <el-option v-for="g in groups.items" :key="g.id" :label="g.name + (g.status !== 'active' ? '（停用）' : '')" :value="g.id" />
      </el-select>
      <template #footer>
        <el-button @click="groupsOpen = false">取消</el-button>
        <el-button type="primary" :loading="groupsSaving" @click="saveGroups">保存</el-button>
      </template>
    </el-dialog>

    <!-- 详情 -->
    <el-drawer v-model="detailOpen" :title="`用户详情：${detail?.username ?? ''}`" size="560px">
      <div v-loading="detailLoading">
        <el-descriptions v-if="detail" :column="2" border size="small" style="margin-bottom: 16px">
          <el-descriptions-item label="邮箱">{{ detail.email }}</el-descriptions-item>
          <el-descriptions-item label="用户名">{{ detail.username }}</el-descriptions-item>
          <el-descriptions-item label="角色">{{ roleLabelMap[detail.role] }}</el-descriptions-item>
          <el-descriptions-item label="状态">{{ statusLabelMap[detail.status] }}</el-descriptions-item>
          <el-descriptions-item label="余额 (USD)"><span class="num">$ {{ detail.balance }}</span></el-descriptions-item>
          <el-descriptions-item label="风险">{{ detail.risk_level }}</el-descriptions-item>
          <el-descriptions-item label="并发上限">{{ detail.concurrency_limit ?? 0 }}（0=跟随分组）</el-descriptions-item>
          <el-descriptions-item label="创建时间">{{ fmtT(detail.created_at) }}</el-descriptions-item>
        </el-descriptions>
        <h4 class="sub-title">API 密钥</h4>
        <el-table :data="detailKeys" style="width: 100%" size="small" empty-text="无密钥">
          <el-table-column prop="name" label="名称" min-width="100" />
          <el-table-column prop="key_prefix" label="前缀" min-width="120"><template #default="{ row }"><code>{{ row.key_prefix }}****</code></template></el-table-column>
          <el-table-column label="状态" width="80"><template #default="{ row }">{{ statusLabelMap[row.status] }}</template></el-table-column>
          <el-table-column label="有效期至" min-width="140"><template #default="{ row }">{{ fmtT(row.expires_at) }}</template></el-table-column>
        </el-table>
      </div>
    </el-drawer>
  </div>
</template>

<style scoped>
.pg-title { font-size: 22px; font-weight: 700; }
.pg-sub { font-size: 13px; color: var(--cf-text-secondary); }
.head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 16px; }
.toolbar { display: flex; gap: 10px; margin-bottom: 14px; flex-wrap: wrap; }
.page-bar { margin-top: 14px; justify-content: flex-end; }
.self-tip { font-size: 12px; color: var(--cf-text-tertiary); }
.d-tip { margin: 0 0 10px; font-size: 13px; color: var(--cf-text-secondary); }
.sub-title { margin: 4px 0 10px; font-size: 14px; font-weight: 600; }

@media (width < 640px) {
  .head { flex-direction: column; }
}
</style>
