<script setup lang="ts">
// 账单与余额（07 §3.1 /me/balance + /me/billing）
definePageMeta({ layout: 'console' })
const balance = ref<{ balance: string; frozen: string; total_recharged: string; total_consumed: string } | null>(null)
const items = ref<any[]>([])
const loading = ref(false)

onMounted(async () => {
  loading.value = true
  try {
    balance.value = await $fetch('/api/v1/me/balance', { credentials: 'include' })
    const b = await $fetch<{ items: any[] }>('/api/v1/me/billing', { credentials: 'include' })
    items.value = b.items || []
  } catch { /* 鉴权失败由 auth.global 兜底；网络错误静默（空态/重试） */ } finally { loading.value = false }
})

function typeTag(t: string) {
  const m: Record<string, string> = { recharge: 'success', settle: 'danger', refund: 'success', adjust: 'info' }
  return m[t] || 'muted'
}
</script>

<template>
  <div>
    <h1 class="pg-title">账单与充值</h1>
    <p class="pg-sub">充值入账与结算流水（当前沙箱支付渠道）</p>

    <el-card v-if="balance" shadow="never" style="margin-top: 16px">
      <div class="grid4">
        <div class="stat"><div class="num stat__v">$ {{ balance.balance }}</div><div class="stat__k">可用余额</div></div>
        <div class="stat"><div class="num stat__v">$ {{ balance.frozen }}</div><div class="stat__k">预扣中</div></div>
        <div class="stat"><div class="num stat__v">$ {{ balance.total_recharged }}</div><div class="stat__k">累计充值</div></div>
        <div class="stat"><div class="num stat__v">$ {{ balance.total_consumed }}</div><div class="stat__k">累计消费</div></div>
      </div>
    </el-card>

    <el-card shadow="never" style="margin-top: 16px">
      <template #header>资金流水</template>
      <el-table v-loading="loading" :data="items" style="width: 100%" empty-text="暂无资金流水">
        <el-table-column label="类型" width="100">
          <template #default="{ row }"><span class="cf-tag" :class="'cf-tag--' + typeTag(row.type)">{{ row.type }}</span></template>
        </el-table-column>
        <el-table-column label="金额 (USD)" width="140"><template #default="{ row }"><span class="num" :style="{ color: row.type === 'settle' ? 'var(--cf-semantic-danger)' : 'var(--cf-semantic-success)' }">{{ row.amount }}</span></template></el-table-column>
        <el-table-column label="余额" width="140"><template #default="{ row }"><span class="num">{{ row.balance_after }}</span></template></el-table-column>
        <el-table-column prop="description" label="说明" min-width="180"><template #default="{ row }">{{ row.description || '—' }}</template></el-table-column>
        <el-table-column label="时间" min-width="170"><template #default="{ row }">{{ formatDateTime(row.created_at) }}</template></el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<style scoped>
.pg-title { font-size: 22px; font-weight: 700; }
.pg-sub { font-size: 13px; color: var(--cf-text-secondary); }
.grid4 { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; }
.stat__v { font-size: 20px; font-weight: 700; }
.stat__k { margin-top: 2px; font-size: 12px; color: var(--cf-text-tertiary); }
</style>
