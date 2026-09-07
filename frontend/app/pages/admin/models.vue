<script setup lang="ts">
// 管理 · 模型目录（只读首屏；写操作随 B4-7 迭代补）
definePageMeta({ layout: 'admin' })
const items = ref<any[]>([])
const loading = ref(true)
onMounted(async () => {
  try {
    const r = await $fetch<{ items: any[] }>('/api/v1/admin/models', { credentials: 'include' })
    items.value = r.items || []
  } catch { ElMessage.error('模型列表加载失败') } finally { loading.value = false }
})
</script>

<template>
  <div>
    <h1 class="pg-title">模型目录</h1>
    <el-card shadow="never" style="margin-top: 16px">
      <el-table v-loading="loading" :data="items" style="width: 100%" empty-text="暂无模型">
        <el-table-column prop="name" label="模型名" min-width="140"><template #default="{ row }"><code>{{ row.name }}</code></template></el-table-column>
        <el-table-column prop="display_name" label="展示名" min-width="140"><template #default="{ row }">{{ row.display_name || '—' }}</template></el-table-column>
        <el-table-column prop="provider_code" label="供应商" width="120" />
        <el-table-column prop="context_window" label="上下文" width="110"><template #default="{ row }"><span class="num">{{ row.context_window?.toLocaleString() ?? '—' }}</span></template></el-table-column>
        <el-table-column prop="max_output_tokens" label="最大输出" width="110"><template #default="{ row }"><span class="num">{{ row.max_output_tokens?.toLocaleString() ?? '—' }}</span></template></el-table-column>
        <el-table-column label="状态" width="90"><template #default="{ row }"><span class="cf-tag" :class="row.status === 'active' ? 'cf-tag--success' : 'cf-tag--muted'">{{ row.status }}</span></template></el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<style scoped>
.pg-title { font-size: 22px; font-weight: 700; }
</style>
