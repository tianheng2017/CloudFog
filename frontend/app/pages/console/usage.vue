<script setup lang="ts">
// 用量（07 §3.1 /me/summary + /me/usage）
definePageMeta({ layout: 'console' })
interface Totals { requests: number; input_tokens: number; output_tokens: number; total_cost: string }
const summary = ref<{ today: Totals; month: Totals } | null>(null)
const items = ref<any[]>([])
const loading = ref(false)

onMounted(async () => {
  loading.value = true
  try {
    summary.value = await $fetch('/api/v1/me/summary', { credentials: 'include' })
    const u = await $fetch<{ items: any[] }>('/api/v1/me/usage', { credentials: 'include' })
    items.value = u.items || []
  } finally { loading.value = false }
})

function statOf(p: Totals) {
  return [
    { label: '请求数', value: String(p?.requests ?? 0) },
    { label: '输入 tokens', value: (p?.input_tokens ?? 0).toLocaleString() },
    { label: '输出 tokens', value: (p?.output_tokens ?? 0).toLocaleString() },
    { label: '费用（USD）', value: p?.total_cost ?? '0' },
  ]
}
</script>

<template>
  <div>
    <h1 class="pg-title">用量</h1>
    <p class="pg-sub">金额/数字为等宽右对齐；统计以 UTC 日界为准</p>

    <el-row v-if="summary" :gutter="16" style="margin-top: 18px">
      <el-col :span="12"><el-card shadow="never">
        <template #header>今日</template>
        <div class="grid4">
          <div v-for="s in statOf(summary.today)" :key="s.label" class="stat"><div class="num stat__v">{{ s.value }}</div><div class="stat__k">{{ s.label }}</div></div>
        </div>
      </el-card></el-col>
      <el-col :span="12"><el-card shadow="never">
        <template #header>本月</template>
        <div class="grid4">
          <div v-for="s in statOf(summary.month)" :key="s.label" class="stat"><div class="num stat__v">{{ s.value }}</div><div class="stat__k">{{ s.label }}</div></div>
        </div>
      </el-card></el-col>
    </el-row>

    <el-card shadow="never" style="margin-top: 16px">
      <template #header>调用明细（最近 20 条）</template>
      <el-table v-loading="loading" :data="items" style="width: 100%" empty-text="暂无调用记录——用官方 SDK 发起一次对话后会显示在这里">
        <el-table-column prop="request_id" label="Request ID" min-width="220"><template #default="{ row }"><code class="num" style="font-size: 12px">{{ row.request_id }}</code></template></el-table-column>
        <el-table-column prop="model" label="模型" min-width="130" />
        <el-table-column label="Tokens (入/出)" width="150"><template #default="{ row }"><span class="num">{{ row.input_tokens ?? 0 }} / {{ row.output_tokens ?? 0 }}</span></template></el-table-column>
        <el-table-column label="费用 (USD)" width="130"><template #default="{ row }"><span class="num">{{ row.total_cost ?? '0' }}</span></template></el-table-column>
        <el-table-column label="时间" min-width="170"><template #default="{ row }">{{ row.created_at || '—' }}</template></el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<style scoped>
.pg-title { font-size: 22px; font-weight: 700; }
.pg-sub { font-size: 13px; color: var(--cf-text-secondary); }
.grid4 { display: grid; grid-template-columns: repeat(4, 1fr); gap: 8px; }
.stat__v { font-size: 18px; font-weight: 650; }
.stat__k { margin-top: 2px; font-size: 12px; color: var(--cf-text-tertiary); }
</style>
