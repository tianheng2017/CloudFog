<script setup lang="ts">
// 用量（07 §3.1 /me/summary + /me/usage）
definePageMeta({ layout: 'console' })
interface Totals { requests: number; input_tokens: number; output_tokens: number; total_cost: string }
interface UsageRow { request_id: string; model: string; input_tokens: number; output_tokens: number; total_cost: string; created_at: string }

const { data: summary } = useApiResource<{ today: Totals; month: Totals } | null>(
  () => apiFetch('/api/v1/me/summary'), null)

// 明细分页（07 §3.1：page/page_size，默认最近 30 天窗口）
const page = ref(1)
const pageSize = ref(20)
const { data: usage, loading, error, reload } = useApiResource<{ items: UsageRow[]; total: number }>(
  () => apiFetch(`/api/v1/me/usage?page=${page.value}&page_size=${pageSize.value}`),
  { items: [], total: 0 })
function onPage(p: number) { page.value = p; void reload() }
function onSize(s: number) { pageSize.value = s; page.value = 1; void reload() }

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
    <p class="pg-sub">金额/数字等宽居中；账期以北京时间为准（UTC+8）</p>

    <div v-if="error" class="err">{{ error }} <el-button link type="primary" @click="reload">重试</el-button></div>

    <el-row v-if="summary" :gutter="16" style="margin-top: 18px">
      <el-col :xs="24" :sm="12"><el-card shadow="never">
        <template #header>今日</template>
        <div class="grid4">
          <div v-for="s in statOf(summary.today)" :key="s.label" class="stat"><div class="num stat__v">{{ s.value }}</div><div class="stat__k">{{ s.label }}</div></div>
        </div>
      </el-card></el-col>
      <el-col :xs="24" :sm="12"><el-card shadow="never">
        <template #header>本月</template>
        <div class="grid4">
          <div v-for="s in statOf(summary.month)" :key="s.label" class="stat"><div class="num stat__v">{{ s.value }}</div><div class="stat__k">{{ s.label }}</div></div>
        </div>
      </el-card></el-col>
    </el-row>

    <el-card shadow="never" style="margin-top: 16px">
      <template #header>调用明细</template>
      <el-table v-loading="loading" :data="usage.items" style="width: 100%" empty-text="暂无调用记录——用官方 SDK 发起一次对话后会显示在这里">
        <el-table-column prop="request_id" label="Request ID" min-width="220"><template #default="{ row }"><code class="num" style="font-size: 12px">{{ row.request_id }}</code></template></el-table-column>
        <el-table-column prop="model" label="模型" min-width="130" />
        <el-table-column label="Tokens (入/出)" width="150"><template #default="{ row }"><span class="num">{{ row.input_tokens ?? 0 }} / {{ row.output_tokens ?? 0 }}</span></template></el-table-column>
        <el-table-column label="费用 (USD)" width="130"><template #default="{ row }"><span class="num">{{ row.total_cost ?? '0' }}</span></template></el-table-column>
        <el-table-column label="时间" min-width="170"><template #default="{ row }">{{ formatDateTime(row.created_at) }}</template></el-table-column>
      </el-table>
      <el-pagination
        v-if="usage.total > pageSize"
        class="page-bar"
        layout="total, prev, pager, next, sizes"
        :total="usage.total"
        :page-size="pageSize"
        :current-page="page"
        :page-sizes="[20, 50, 100]"
        @current-change="onPage"
        @size-change="onSize"
      />
    </el-card>
  </div>
</template>

<style scoped>
.pg-title { font-size: 22px; font-weight: 700; }
.pg-sub { font-size: 13px; color: var(--cf-text-secondary); }

/* 窄屏自适应（2026-09-08）：固定 4 列在 <640px 会挤压溢出 */
.grid4 { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); gap: 8px; }
.err { margin-top: 12px; font-size: 13px; color: var(--cf-semantic-danger); }
.stat__v { font-size: 18px; font-weight: 650; }
.stat__k { margin-top: 2px; font-size: 12px; color: var(--cf-text-tertiary); }
.page-bar { margin-top: 14px; justify-content: flex-end; }
</style>
