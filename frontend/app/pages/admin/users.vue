<script setup lang="ts">
// 管理 · 用户（只读首屏）
definePageMeta({ layout: 'admin' })
const items = ref<any[]>([])
const loading = ref(true)
onMounted(async () => {
  try {
    const r = await $fetch<{ items: any[] }>('/api/v1/admin/users', { credentials: 'include' })
    items.value = r.items || []
  } catch { ElMessage.error('用户列表加载失败') } finally { loading.value = false }
})
function roleTag(role: string) {
  if (role === 'super_admin') return 'danger'
  if (role === 'admin') return 'warning'
  return 'muted'
}
</script>

<template>
  <div>
    <h1 class="pg-title">用户</h1>
    <el-card shadow="never" style="margin-top: 16px">
      <el-table v-loading="loading" :data="items" style="width: 100%" empty-text="暂无用户">
        <el-table-column prop="id" label="ID" width="70"><template #default="{ row }"><span class="num">{{ row.id }}</span></template></el-table-column>
        <el-table-column prop="username" label="用户名" min-width="140" />
        <el-table-column prop="email" label="邮箱" min-width="180"><template #default="{ row }">{{ row.email || '—' }}</template></el-table-column>
        <el-table-column label="角色" width="120"><template #default="{ row }"><span class="cf-tag" :class="'cf-tag--' + roleTag(row.role)">{{ row.role }}</span></template></el-table-column>
        <el-table-column label="状态" width="100"><template #default="{ row }"><span class="cf-tag" :class="row.status === 'active' ? 'cf-tag--success' : 'cf-tag--muted'">{{ row.status }}</span></template></el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<style scoped>
.pg-title { font-size: 22px; font-weight: 700; }
</style>
