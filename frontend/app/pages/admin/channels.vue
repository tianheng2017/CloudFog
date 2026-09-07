<script setup lang="ts">
// 管理 · 渠道（只读首屏；凭证绝不显示）
definePageMeta({ layout: 'admin' })
const items = ref<any[]>([])
const loading = ref(true)
onMounted(async () => {
  try {
    const r = await $fetch<{ items: any[] }>('/api/v1/admin/channels', { credentials: 'include' })
    items.value = r.items || []
  } catch { ElMessage.error('渠道列表加载失败') } finally { loading.value = false }
})
</script>

<template>
  <div>
    <h1 class="pg-title">渠道</h1>
    <el-card shadow="never" style="margin-top: 16px">
      <el-table v-loading="loading" :data="items" style="width: 100%" empty-text="暂无渠道">
        <el-table-column prop="id" label="ID" width="70"><template #default="{ row }"><span class="num">{{ row.id }}</span></template></el-table-column>
        <el-table-column prop="name" label="名称" min-width="150" />
        <el-table-column prop="provider_code" label="供应商" width="120" />
        <el-table-column label="倍率" width="100"><template #default="{ row }"><span class="num">{{ row.rate_multiplier ?? '1' }}</span></template></el-table-column>
        <el-table-column label="可调度" width="90"><template #default="{ row }">{{ row.schedulable ? '是' : '否' }}</template></el-table-column>
        <el-table-column label="状态" width="100"><template #default="{ row }"><span class="cf-tag" :class="row.status === 'active' ? 'cf-tag--success' : 'cf-tag--muted'">{{ row.status }}</span></template></el-table-column>
        <el-table-column label="凭证" width="110"><template #default="{ row }"><span class="cf-tag cf-tag--muted">{{ row.cred?.encrypted ? '已加密' : '—' }}</span></template></el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<style scoped>
.pg-title { font-size: 22px; font-weight: 700; }
</style>
