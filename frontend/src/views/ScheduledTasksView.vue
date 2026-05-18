<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { message } from 'ant-design-vue'
import { ReloadOutlined, SaveOutlined } from '@ant-design/icons-vue'

import {
  listScheduledTasks,
  updateScheduledTask,
  type ScheduledTaskDTO,
} from '@/api/client'

const loading = ref(false)
const saving = ref<Record<string, boolean>>({})
const tasks = ref<ScheduledTaskDTO[]>([])
const drafts = reactive<Record<string, { enabled: boolean; intervalMinutes: number }>>({})

const columns = [
  { title: '任务', key: 'task' },
  { title: '状态', key: 'enabled', width: 110 },
  { title: '间隔', key: 'interval', width: 190 },
  { title: '上次运行', key: 'lastRunAt', width: 180 },
  { title: '下次运行', key: 'nextRunAt', width: 180 },
  { title: '最近错误', key: 'lastError' },
  { title: '操作', key: 'action', width: 110 },
]

async function load() {
  loading.value = true
  try {
    const next = await listScheduledTasks()
    tasks.value = next
    for (const task of next) {
      drafts[task.id] = {
        enabled: task.enabled,
        intervalMinutes: task.intervalMinutes,
      }
    }
  } catch (error) {
    message.error(error instanceof Error ? error.message : '加载定时任务失败')
  } finally {
    loading.value = false
  }
}

async function saveTask(task: ScheduledTaskDTO) {
  const draft = drafts[task.id]
  if (!draft) return
  if (!draft.intervalMinutes || draft.intervalMinutes < 1 || draft.intervalMinutes > 1440) {
    message.warning('定时间隔必须在 1 到 1440 分钟之间')
    return
  }
  saving.value = { ...saving.value, [task.id]: true }
  try {
    const updated = await updateScheduledTask(task.id, {
      enabled: draft.enabled,
      intervalMinutes: draft.intervalMinutes,
    })
    const index = tasks.value.findIndex((item) => item.id === updated.id)
    if (index >= 0) {
      tasks.value[index] = updated
    }
    drafts[updated.id] = {
      enabled: updated.enabled,
      intervalMinutes: updated.intervalMinutes,
    }
    message.success('定时任务已保存')
  } catch (error) {
    message.error(error instanceof Error ? error.message : '保存定时任务失败')
  } finally {
    saving.value = { ...saving.value, [task.id]: false }
  }
}

function formatDate(value?: string) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

function statusColor(task: ScheduledTaskDTO) {
  return task.enabled ? 'processing' : 'default'
}

function statusText(task: ScheduledTaskDTO) {
  return task.enabled ? '已开启' : '已关闭'
}

onMounted(load)
</script>

<template>
  <div class="page">
    <section class="page-header">
      <div>
        <div class="header-kicker">Scheduled Tasks</div>
        <h1>定时任务</h1>
      </div>
      <a-button :loading="loading" @click="load">
        <template #icon><ReloadOutlined /></template>
        刷新
      </a-button>
    </section>

    <a-card :loading="loading" :bordered="false" class="panel">
      <a-table
        class="desktop-table"
        row-key="id"
        :columns="columns"
        :data-source="tasks"
        :pagination="false"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'task'">
            <div class="task-title">{{ record.name }}</div>
            <div class="muted">{{ record.description }}</div>
          </template>
          <template v-else-if="column.key === 'enabled'">
            <a-switch v-model:checked="drafts[record.id].enabled" checked-children="开" un-checked-children="关" />
          </template>
          <template v-else-if="column.key === 'interval'">
            <a-input-number
              v-model:value="drafts[record.id].intervalMinutes"
              style="width: 100%"
              :min="1"
              :max="1440"
              addon-after="分钟"
            />
          </template>
          <template v-else-if="column.key === 'lastRunAt'">
            {{ formatDate(record.lastRunAt) }}
          </template>
          <template v-else-if="column.key === 'nextRunAt'">
            {{ record.enabled ? formatDate(record.nextRunAt) : '-' }}
          </template>
          <template v-else-if="column.key === 'lastError'">
            <span :class="{ 'error-copy': record.lastError }">{{ record.lastError || '-' }}</span>
          </template>
          <template v-else-if="column.key === 'action'">
            <a-button type="primary" :loading="saving[record.id]" @click="saveTask(record)">
              <template #icon><SaveOutlined /></template>
              保存
            </a-button>
          </template>
        </template>
      </a-table>

      <div class="mobile-list">
        <div v-if="tasks.length === 0" class="mobile-empty">暂无定时任务</div>
        <div v-for="task in tasks" :key="task.id" class="mobile-card">
          <div class="mobile-card-top">
            <div>
              <div class="task-title">{{ task.name }}</div>
              <div class="muted">{{ task.description }}</div>
            </div>
            <a-tag :color="statusColor(task)">{{ statusText(task) }}</a-tag>
          </div>
          <a-form layout="vertical">
            <a-form-item label="启用">
              <a-switch v-model:checked="drafts[task.id].enabled" checked-children="开" un-checked-children="关" />
            </a-form-item>
            <a-form-item label="定时间隔">
              <a-input-number
                v-model:value="drafts[task.id].intervalMinutes"
                style="width: 100%"
                :min="1"
                :max="1440"
                addon-after="分钟"
              />
            </a-form-item>
          </a-form>
          <div class="mobile-meta">
            <span>上次运行：{{ formatDate(task.lastRunAt) }}</span>
            <span>下次运行：{{ task.enabled ? formatDate(task.nextRunAt) : '-' }}</span>
            <span v-if="task.lastError" class="error-copy">错误：{{ task.lastError }}</span>
          </div>
          <a-button type="primary" block :loading="saving[task.id]" @click="saveTask(task)">
            <template #icon><SaveOutlined /></template>
            保存
          </a-button>
        </div>
      </div>
    </a-card>
  </div>
</template>

<style scoped>
.page {
  display: grid;
  gap: 24px;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
}

.header-kicker {
  text-transform: uppercase;
  letter-spacing: 0.12em;
  color: #6f89a4;
  font-weight: 700;
  margin-bottom: 8px;
}

h1 {
  margin: 0;
}

.panel {
  background: rgba(255, 255, 255, 0.82);
  border-radius: 18px;
}

.task-title {
  font-weight: 700;
}

.muted {
  color: #6f89a4;
  font-size: 12px;
}

.error-copy {
  color: #cf1322;
  font-size: 12px;
  word-break: break-all;
}

.mobile-list {
  display: none;
}

@media (max-width: 760px) {
  .page-header {
    display: grid;
  }

  .desktop-table {
    display: none;
  }

  .mobile-list {
    display: grid;
    gap: 12px;
  }

  .mobile-card {
    display: grid;
    gap: 12px;
    padding: 14px;
    border: 1px solid rgba(22, 32, 51, 0.08);
    border-radius: 12px;
    background: rgba(255, 255, 255, 0.72);
  }

  .mobile-card-top {
    display: flex;
    justify-content: space-between;
    gap: 12px;
  }

  .mobile-meta {
    display: grid;
    gap: 4px;
    color: #58708f;
    font-size: 12px;
  }

  .mobile-empty {
    color: #6f89a4;
    text-align: center;
    padding: 24px;
  }
}
</style>
