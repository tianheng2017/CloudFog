<script setup lang="ts">
// 账单与充值（07 §3.1 /me/balance + /me/billing + payment 充值闭环）
// 金额一律后端 decimal 字符串：前端不 Number 运算、直接展示/回传，避免精度丢失。
definePageMeta({ layout: 'console' })
interface Balance { balance: string; frozen: string; total_recharged: string; total_consumed: string }
interface LedgerRow { type: string; amount: string; balance_after: string; description: string; created_at: string }
interface PayProvider { code: string; name: string }
interface PayOrder { order_no: string; amount: string; currency: string; provider: string; status: string; pay_url: string; expires_at: string }

// ── 余额卡 ──
const bal = ref<Balance | null>(null)
const balLoading = ref(false)
async function loadBalance() {
  balLoading.value = true
  try {
    bal.value = await apiFetch<Balance>('/api/v1/me/balance')
  } catch { /* 401 已统一跳登录 */ } finally { balLoading.value = false }
}
onMounted(loadBalance)

// ── 资金流水（分页 + type 筛选）──
const page = ref(1)
const pageSize = ref(10)
const typeFilter = ref('')
const { data: ledger, loading: ledgerLoading, error: ledgerError, reload: reloadLedger } = useApiResource<{ items: LedgerRow[]; total: number }>(
  () => {
    const q = new URLSearchParams({ page: String(page.value), page_size: String(pageSize.value) })
    if (typeFilter.value) q.set('type', typeFilter.value)
    return apiFetch(`/api/v1/me/billing?${q}`)
  },
  { items: [], total: 0 })
function onPage(p: number) { page.value = p; void reloadLedger() }
function onSize(s: number) { pageSize.value = s; page.value = 1; void reloadLedger() }
function onType(v: string) { typeFilter.value = v; page.value = 1; void reloadLedger() }

// ── 充值下单（07 §3.2：仅 USD，amount 为十进制字符串）──
const providers = ref<PayProvider[]>([])
const provLoading = ref(false)
const chargeOpen = ref(false)
const charging = ref(false)
const chargeError = ref('')
const amountPresets = ['10', '50', '100', '500']
const amount = ref('50')
const providerCode = ref('')
const order = ref<PayOrder | null>(null)
const paying = ref(false)
const payDone = ref(false)

async function loadProviders() {
  provLoading.value = true
  try {
    const r = await apiFetch<{ items: PayProvider[] }>('/api/v1/payment/providers')
    providers.value = r.items || []
    const first = providers.value[0]
    if (!providerCode.value && first) providerCode.value = first.code
  } catch { /* 打开展开时给空态提示 */ } finally { provLoading.value = false }
}
function openCharge() {
  chargeError.value = ''
  order.value = null
  payDone.value = false
  amount.value = '50'
  void loadProviders()
  chargeOpen.value = true
}

async function createOrder() {
  const amt = amount.value.trim()
  if (!/^\d+(\.\d{1,2})?$/.test(amt) || Number(amt) <= 0 || Number(amt) > 5000) {
    chargeError.value = '金额需为 0 < 金额 ≤ 5000 的正数（最多两位小数）'
    return
  }
  charging.value = true
  chargeError.value = ''
  try {
    order.value = await apiFetch<PayOrder>('/api/v1/payment/orders', {
      method: 'POST',
      body: { amount: amt, currency: 'USD', ...(providerCode.value ? { provider_code: providerCode.value } : {}) },
    })
    payDone.value = false
  } catch (e: any) {
    chargeError.value = apiMessage(e, '下单失败')
  } finally { charging.value = false }
}

async function copyText(s: string, okMsg: string) {
  try {
    if (!navigator.clipboard) throw new Error('unsupported')
    await navigator.clipboard.writeText(s)
    ElMessage.success(okMsg)
  } catch {
    ElMessage.warning('自动复制失败，请手动复制')
  }
}

// mock 渠道演练：直接触发一次模拟回调入账（mock 无签名，仅 dev/演练注入；生产无此渠道）
async function simulatePaid() {
  if (!order.value) return
  paying.value = true
  try {
    await apiFetch('/api/v1/payment/notify/mock', {
      method: 'POST',
      body: {
        order_no: order.value.order_no,
        provider_trade_no: `mock-${order.value.order_no}`,
        amount: order.value.amount,
      },
    })
    // 入账为异步任务（RabbitMQ confirm worker 消费），轮询订单至 paid 后刷新
    payDone.value = false
    for (let i = 0; i < 15; i++) {
      await new Promise(r => setTimeout(r, 1000))
      const o = await apiFetch<PayOrder>(`/api/v1/payment/orders/${order.value.order_no}`).catch(() => null)
      if (o && o.status === 'paid') { payDone.value = true; break }
    }
    await loadBalance()
    void reloadLedger()
  } catch (e: any) {
    ElMessage.error(apiMessage(e, '模拟支付失败'))
  } finally { paying.value = false }
}

function typeTag(t: string) {
  const m: Record<string, string> = { recharge: 'success', settle: 'danger', refund: 'success', adjust: 'info' }
  return m[t] || 'muted'
}
function typeLabel(t: string) {
  const m: Record<string, string> = { recharge: '充值', settle: '消费', refund: '退回', adjust: '调账' }
  return m[t] ?? t
}
const typeOptions = [
  { value: '', label: '全部' },
  { value: 'recharge', label: '充值' },
  { value: 'settle', label: '消费' },
  { value: 'refund', label: '退回' },
  { value: 'adjust', label: '调账' },
]
</script>

<template>
  <div>
    <div class="head">
      <div><h1 class="pg-title">账单与充值</h1><p class="pg-sub">充值入账与结算流水；账期以北京时间为准（UTC+8）</p></div>
      <el-button type="primary" :loading="balLoading" @click="openCharge">充值</el-button>
    </div>

    <div v-if="ledgerError" class="err">{{ ledgerError }} <el-button link type="primary" @click="reloadLedger">重试</el-button></div>

    <el-card v-if="bal" shadow="never" style="margin-top: 16px">
      <div class="grid4">
        <div class="stat"><div class="num stat__v">$ {{ bal.balance }}</div><div class="stat__k">可用余额</div></div>
        <div class="stat"><div class="num stat__v">$ {{ bal.frozen }}</div><div class="stat__k">预扣中</div></div>
        <div class="stat"><div class="num stat__v">$ {{ bal.total_recharged }}</div><div class="stat__k">累计充值</div></div>
        <div class="stat"><div class="num stat__v">$ {{ bal.total_consumed }}</div><div class="stat__k">累计消费</div></div>
      </div>
    </el-card>

    <el-card shadow="never" style="margin-top: 16px">
      <template #header>
        <div class="card-head">
          <span>资金流水</span>
          <el-select v-model="typeFilter" size="small" style="width: 120px" @change="onType">
            <el-option v-for="o in typeOptions" :key="o.value" :label="o.label" :value="o.value" />
          </el-select>
        </div>
      </template>
      <el-table v-loading="ledgerLoading" :data="ledger.items" style="width: 100%" empty-text="暂无资金流水">
        <el-table-column label="类型" width="100">
          <template #default="{ row }"><span class="cf-tag" :class="'cf-tag--' + typeTag(row.type)">{{ typeLabel(row.type) }}</span></template>
        </el-table-column>
        <el-table-column label="金额 (USD)" width="150">
          <template #default="{ row }"><span class="num" :style="{ color: row.type === 'settle' ? 'var(--cf-semantic-danger)' : 'var(--cf-semantic-success)' }">{{ row.amount.startsWith('-') ? row.amount : '+' + row.amount }}</span></template>
        </el-table-column>
        <el-table-column label="余额" width="140"><template #default="{ row }"><span class="num">{{ row.balance_after }}</span></template></el-table-column>
        <el-table-column prop="description" label="说明" min-width="200"><template #default="{ row }">{{ row.description || '—' }}</template></el-table-column>
        <el-table-column label="时间" min-width="170"><template #default="{ row }">{{ formatDateTime(row.created_at) }}</template></el-table-column>
      </el-table>
      <el-pagination
        v-if="ledger.total > pageSize"
        class="page-bar"
        layout="total, prev, pager, next"
        :total="ledger.total"
        :page-size="pageSize"
        :current-page="page"
        @current-change="onPage"
      />
    </el-card>

    <!-- 充值下单 -->
    <el-dialog v-model="chargeOpen" title="账户充值" width="480px">
      <template v-if="!order">
        <p class="d-sub">金额以美元（USD）计；确认后创建订单并在所选渠道完成付款。</p>
        <el-form label-position="top">
          <el-form-item label="金额（USD）">
            <el-radio-group v-model="amount">
              <el-radio-button v-for="a in amountPresets" :key="a" :value="a">$ {{ a }}</el-radio-button>
            </el-radio-group>
            <el-input v-model="amount" class="amt-input" placeholder="自定义金额：0 < 金额 ≤ 5000" />
          </el-form-item>
          <el-form-item v-if="providers.length" label="支付渠道">
            <el-select v-model="providerCode" style="width: 100%">
              <el-option v-for="p in providers" :key="p.code" :label="p.name" :value="p.code" />
            </el-select>
          </el-form-item>
        </el-form>
        <el-alert v-if="!providers.length && !provLoading" type="warning" :closable="false" title="暂未配置可用支付渠道，请联系管理员" style="margin-bottom: 8px" />
      </template>

      <el-alert v-if="chargeError" :title="chargeError" type="error" :closable="false" style="margin-bottom: 8px" />
      <el-alert v-if="order && order.provider === 'mock'" type="warning" :closable="false" title="当前为模拟支付渠道（开发/演练），可直接点下方按钮模拟到账">
        <template #default>
          <div class="o-line">订单号 <code>{{ order.order_no }}</code></div>
          <div class="o-line">金额 $ {{ order.amount }} · 有效期至 {{ formatDateTime(order.expires_at) }}</div>
        </template>
      </el-alert>
      <el-alert v-else-if="order" type="info" :closable="false">
        <template #title>订单已创建（渠道：{{ order.provider }}）</template>
        <template #default>
          <div class="o-line">订单号 <code>{{ order.order_no }}</code> <el-button link type="primary" @click="copyText(order.order_no, '订单号已复制')">复制</el-button></div>
          <div class="o-line">支付地址 <a :href="order.pay_url" target="_blank" rel="noopener">{{ order.pay_url }}</a> <el-button link type="primary" @click="copyText(order.pay_url, '支付链接已复制')">复制</el-button></div>
          <div class="o-line">金额 $ {{ order.amount }} · 有效期至 {{ formatDateTime(order.expires_at) }}</div>
        </template>
      </el-alert>

      <template #footer>
        <el-button @click="chargeOpen = false">关闭</el-button>
        <el-button v-if="!order" type="primary" :loading="charging || provLoading" @click="createOrder">创建订单</el-button>
        <template v-else-if="order.provider === 'mock'">
          <el-button type="primary" :loading="paying" :disabled="payDone" @click="simulatePaid">{{ payDone ? '已到账' : '模拟支付完成' }}</el-button>
        </template>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 20px; }
.pg-title { font-size: 22px; font-weight: 700; }
.pg-sub { font-size: 13px; color: var(--cf-text-secondary); }
.card-head { display: flex; align-items: center; justify-content: space-between; }
.grid4 { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); gap: 12px; }
.stat__v { font-size: 20px; font-weight: 700; }
.stat__k { margin-top: 2px; font-size: 12px; color: var(--cf-text-tertiary); }
.err { margin-top: 12px; font-size: 13px; color: var(--cf-semantic-danger); }
.d-sub { margin-bottom: 10px; font-size: 13px; color: var(--cf-text-secondary); }
.amt-input { margin-top: 10px; }
.o-line { margin: 4px 0; font-size: 13px; word-break: break-all; }
.page-bar { margin-top: 14px; justify-content: flex-end; }

@media (width < 640px) {
  .head { flex-direction: column; }
}
</style>
