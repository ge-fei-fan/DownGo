<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { message, Modal } from 'ant-design-vue'
import { DeleteOutlined, FolderOpenOutlined, RedoOutlined, StopOutlined } from '@ant-design/icons-vue'

import {
  cancelDownload,
  createDownload,
  deleteDownload,
  inspectDownload,
  openDownloadPath,
  retryDownload,
  type DownloadItem,
  type InspectResult,
} from '@/api/client'
import { useDownloadsStore } from '@/stores/downloads'

const downloads = useDownloadsStore()

const loadingInspect = ref(false)
const loadingCreate = ref(false)
const activeTab = ref<'active' | 'completed'>('active')
const form = reactive({ url: '' })
const inspect = ref<InspectResult | null>(null)

const activeColumns = [
  { title: '视频', key: 'video' },
  { title: '状态', dataIndex: 'status', key: 'status', width: 120 },
  { title: '清晰度', key: 'quality', width: 120 },
  { title: '进度', key: 'progress', width: 220 },
  { title: '速度', key: 'speed', width: 140 },
  { title: '剩余时间', key: 'eta', width: 110 },
  { title: '创建时间', dataIndex: 'createdAt', key: 'createdAt', width: 180 },
  { title: '操作', key: 'action', width: 180 },
]

const completedColumns = [
  { title: '视频', key: 'video' },
  { title: '清晰度', key: 'quality', width: 120 },
  { title: '保存文件', dataIndex: 'outputFilename', key: 'outputFilename' },
  { title: '文件路径', dataIndex: 'outputPath', key: 'outputPath' },
  { title: '完成时间', dataIndex: 'completedAt', key: 'completedAt', width: 180 },
  { title: '操作', key: 'action', width: 160 },
]

const activeList = computed(() => downloads.activeList)
const completedList = computed(() => downloads.completedList)

const duplicateText = computed(() => {
  if (!inspect.value?.duplicateOf) {
    return ''
  }
  return `该视频已存在任务：${inspect.value.duplicateOf.title}`
})

async function parseURL() {
  if (!form.url.trim()) {
    message.warning('请先粘贴 YouTube 视频或 Shorts 链接')
    return
  }
  loadingInspect.value = true
  try {
    inspect.value = await inspectDownload(form.url.trim())
  } catch (error) {
    inspect.value = null
    message.error(error instanceof Error ? error.message : '解析链接失败')
  } finally {
    loadingInspect.value = false
  }
}

async function submitDownload() {
  if (!form.url.trim()) {
    return
  }
  loadingCreate.value = true
  try {
    await createDownload(form.url.trim())
    message.success('下载任务已加入队列')
    form.url = ''
    inspect.value = null
  } catch (error) {
    const err = error as Error & { code?: string; item?: DownloadItem }
    if (err.code === 'DOWNLOAD_ALREADY_EXISTS' && err.item) {
      message.warning(`已存在任务：${err.item.title}`)
      inspect.value = { ...(inspect.value as InspectResult), duplicateOf: err.item }
    } else {
      message.error(err.message)
    }
  } finally {
    loadingCreate.value = false
  }
}

async function confirmDelete(item: DownloadItem) {
  Modal.confirm({
    title: '删除记录和文件？',
    content: '数据库记录会被软删除，关联文件也会从磁盘中移除。',
    okText: '确认删除',
    okType: 'danger',
    async onOk() {
      await deleteDownload(item.id)
      message.success('已删除')
    },
  })
}

async function stopTask(item: DownloadItem) {
  await cancelDownload(item.id)
  message.success('任务已取消')
}

async function retryTask(item: DownloadItem) {
  await retryDownload(item.id)
  message.success('任务已重新加入队列')
}

async function openPath(item: DownloadItem) {
  try {
    await openDownloadPath(item.id)
  } catch (error) {
    message.error(error instanceof Error ? error.message : '打开文件路径失败')
  }
}

function formatSpeed(value: number) {
  if (!value) {
    return '-'
  }
  const mib = value / 1024 / 1024
  return `${mib.toFixed(2)} MiB/s`
}

function formatETA(value: number) {
  if (!value) {
    return '-'
  }
  const minutes = Math.floor(value / 60)
  const seconds = value % 60
  return `${minutes}分 ${seconds}秒`
}

function formatDate(value?: string) {
  if (!value) {
    return '-'
  }
  return new Date(value).toLocaleString()
}

function isRecordActive(record: DownloadItem) {
  return record.status === 'queued' || record.status === 'downloading' || record.status === 'postprocessing'
}

function formatStatus(status: string) {
  switch (status) {
    case 'queued':
      return '排队中'
    case 'downloading':
      return '下载中'
    case 'postprocessing':
      return '处理中'
    case 'completed':
      return '已完成'
    case 'failed':
      return '失败'
    case 'canceled':
      return '已取消'
    default:
      return status
  }
}

function formatQuality(item: Pick<DownloadItem, 'qualityLabel' | 'container'>) {
  const quality = item.qualityLabel?.trim()
  const container = item.container?.trim()
  if (quality && container) {
    return `${quality} / ${container}`
  }
  if (quality) {
    return quality
  }
  if (container) {
    return container
  }
  return '-'
}

onMounted(() => {
  downloads.loadActive()
  downloads.loadCompleted()
  downloads.connect()
})

onBeforeUnmount(() => {
  downloads.disconnect()
})
</script>

<template>
  <div class="page">
    <section class="hero">
      <div>
        <div class="hero-kicker">YouTube / Shorts</div>
        <h1>添加下载任务，并实时查看当前进度与清晰度。</h1>
      </div>
      <a-card class="queue-card" :bordered="false">
        <a-space direction="vertical" style="width: 100%" size="middle">
          <a-input
            v-model:value="form.url"
            size="large"
            placeholder="https://www.youtube.com/watch?v=..."
            @press-enter="parseURL"
          />
          <a-space wrap>
            <a-button :loading="loadingInspect" @click="parseURL">解析链接</a-button>
            <a-button type="primary" :loading="loadingCreate" @click="submitDownload">
              加入下载队列
            </a-button>
          </a-space>
          <a-alert v-if="duplicateText" type="warning" show-icon :message="duplicateText" />
          <a-card v-if="inspect" size="small">
            <a-space align="start" class="inspect-layout">
              <a-image :width="160" :src="inspect.thumbnailUrl" :preview="false" />
              <div class="inspect-copy">
                <div class="inspect-title">{{ inspect.title }}</div>
                <div>视频 ID：{{ inspect.videoId }}</div>
                <div>预计大小：{{ (inspect.estimatedSizeBytes / 1024 / 1024).toFixed(2) }} MiB</div>
                <div>预计清晰度：{{ formatQuality(inspect) }}</div>
                <div>建议文件名：{{ inspect.suggestedFilename }}</div>
              </div>
            </a-space>
          </a-card>
        </a-space>
      </a-card>
    </section>

    <a-card :bordered="false">
      <a-tabs v-model:activeKey="activeTab">
        <a-tab-pane key="active" tab="下载中">
          <a-table
            class="desktop-table"
            row-key="id"
            :columns="activeColumns"
            :data-source="activeList.items"
            :pagination="{ current: activeList.page, pageSize: activeList.pageSize, total: activeList.total, onChange: downloads.loadActive }"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'video'">
                <div class="video-cell">
                  <a-image :width="120" :src="record.thumbnailUrl" :preview="false" />
                  <div>
                    <div class="video-title">{{ record.title }}</div>
                    <div class="muted">{{ record.videoId }}</div>
                    <div v-if="record.errorMessage" class="error-copy">{{ record.errorMessage }}</div>
                  </div>
                </div>
              </template>
              <template v-else-if="column.key === 'status'">
                {{ formatStatus(record.status) }}
              </template>
              <template v-else-if="column.key === 'quality'">
                <a-tag color="blue">{{ formatQuality(record) }}</a-tag>
              </template>
              <template v-else-if="column.key === 'progress'">
                <a-progress
                  :percent="Math.round(record.progressPercent)"
                  :stroke-color="record.status === 'postprocessing' ? '#fa8c16' : undefined"
                />
              </template>
              <template v-else-if="column.key === 'speed'">
                {{ formatSpeed(record.speedBps) }}
              </template>
              <template v-else-if="column.key === 'eta'">
                {{ formatETA(record.etaSeconds) }}
              </template>
              <template v-else-if="column.key === 'createdAt'">
                {{ formatDate(record.createdAt) }}
              </template>
              <template v-else-if="column.key === 'action'">
                <a-space>
                  <a-tooltip title="取消任务">
                    <a-button size="small" @click="stopTask(record)">
                      <template #icon><StopOutlined /></template>
                    </a-button>
                  </a-tooltip>
                  <a-tooltip title="重新下载">
                    <a-button size="small" :disabled="isRecordActive(record)" @click="retryTask(record)">
                      <template #icon><RedoOutlined /></template>
                    </a-button>
                  </a-tooltip>
                  <a-tooltip title="删除记录和文件">
                    <a-button danger size="small" @click="confirmDelete(record)">
                      <template #icon><DeleteOutlined /></template>
                    </a-button>
                  </a-tooltip>
                </a-space>
              </template>
            </template>
          </a-table>

          <div class="mobile-list">
            <div v-if="activeList.items.length === 0" class="mobile-empty">暂无下载中的任务</div>
            <div v-for="record in activeList.items" :key="record.id" class="mobile-card">
              <div class="mobile-card-top">
                <a-image :width="96" :src="record.thumbnailUrl" :preview="false" />
                <div class="mobile-copy">
                  <div class="video-title">{{ record.title }}</div>
                  <div class="muted">{{ record.videoId }}</div>
                  <div class="mobile-status">{{ formatStatus(record.status) }}</div>
                </div>
              </div>
              <a-progress
                :percent="Math.round(record.progressPercent)"
                :stroke-color="record.status === 'postprocessing' ? '#fa8c16' : undefined"
              />
              <div class="mobile-meta">
                <span>清晰度：{{ formatQuality(record) }}</span>
                <span>速度：{{ formatSpeed(record.speedBps) }}</span>
                <span>剩余：{{ formatETA(record.etaSeconds) }}</span>
                <span>创建：{{ formatDate(record.createdAt) }}</span>
              </div>
              <div v-if="record.errorMessage" class="error-copy">{{ record.errorMessage }}</div>
              <a-space wrap>
                <a-button size="small" @click="stopTask(record)">
                  <template #icon><StopOutlined /></template>
                  取消
                </a-button>
                <a-button size="small" :disabled="isRecordActive(record)" @click="retryTask(record)">
                  <template #icon><RedoOutlined /></template>
                  重试
                </a-button>
                <a-button danger size="small" @click="confirmDelete(record)">
                  <template #icon><DeleteOutlined /></template>
                  删除
                </a-button>
              </a-space>
            </div>

            <a-pagination
              v-if="activeList.total > activeList.pageSize"
              class="mobile-pagination"
              :current="activeList.page"
              :page-size="activeList.pageSize"
              :total="activeList.total"
              simple
              @change="downloads.loadActive"
            />
          </div>
        </a-tab-pane>

        <a-tab-pane key="completed" tab="已完成">
          <a-table
            class="desktop-table"
            row-key="id"
            :columns="completedColumns"
            :data-source="completedList.items"
            :pagination="{ current: completedList.page, pageSize: completedList.pageSize, total: completedList.total, onChange: downloads.loadCompleted }"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'video'">
                <div class="video-cell">
                  <a-image :width="120" :src="record.thumbnailUrl" :preview="false" />
                  <div class="video-title">{{ record.title }}</div>
                </div>
              </template>
              <template v-else-if="column.key === 'quality'">
                <a-tag color="blue">{{ formatQuality(record) }}</a-tag>
              </template>
              <template v-else-if="column.key === 'completedAt'">
                {{ formatDate(record.completedAt) }}
              </template>
              <template v-else-if="column.key === 'action'">
                <a-space>
                  <a-tooltip title="打开文件路径">
                    <a-button size="small" @click="openPath(record)">
                      <template #icon><FolderOpenOutlined /></template>
                    </a-button>
                  </a-tooltip>
                  <a-tooltip title="删除记录和文件">
                    <a-button danger size="small" @click="confirmDelete(record)">
                      <template #icon><DeleteOutlined /></template>
                    </a-button>
                  </a-tooltip>
                </a-space>
              </template>
            </template>
          </a-table>

          <div class="mobile-list">
            <div v-if="completedList.items.length === 0" class="mobile-empty">暂无已完成任务</div>
            <div v-for="record in completedList.items" :key="record.id" class="mobile-card">
              <div class="mobile-card-top">
                <a-image :width="96" :src="record.thumbnailUrl" :preview="false" />
                <div class="mobile-copy">
                  <div class="video-title">{{ record.title }}</div>
                  <div class="muted">完成时间：{{ formatDate(record.completedAt) }}</div>
                </div>
              </div>
              <div class="mobile-meta">
                <span>清晰度：{{ formatQuality(record) }}</span>
                <span>文件：{{ record.outputFilename }}</span>
                <span>路径：{{ record.outputPath }}</span>
              </div>
              <a-space wrap>
                <a-button size="small" @click="openPath(record)">
                  <template #icon><FolderOpenOutlined /></template>
                  打开文件路径
                </a-button>
                <a-button danger size="small" @click="confirmDelete(record)">
                  <template #icon><DeleteOutlined /></template>
                  删除
                </a-button>
              </a-space>
            </div>

            <a-pagination
              v-if="completedList.total > completedList.pageSize"
              class="mobile-pagination"
              :current="completedList.page"
              :page-size="completedList.pageSize"
              :total="completedList.total"
              simple
              @change="downloads.loadCompleted"
            />
          </div>
        </a-tab-pane>
      </a-tabs>
    </a-card>
  </div>
</template>

<style scoped>
.page {
  display: grid;
  gap: 24px;
}

.hero {
  display: grid;
  gap: 24px;
  grid-template-columns: 1.2fr 1fr;
  align-items: start;
}

.hero-kicker {
  text-transform: uppercase;
  letter-spacing: 0.12em;
  color: #6f89a4;
  font-weight: 700;
  margin-bottom: 12px;
}

.hero h1 {
  margin: 0;
  font-size: clamp(32px, 5vw, 52px);
  line-height: 1.02;
  max-width: 12ch;
}

.queue-card {
  background: rgba(255, 255, 255, 0.78);
  border-radius: 18px;
  box-shadow: 0 16px 44px rgba(21, 43, 73, 0.11);
}

.inspect-layout {
  width: 100%;
}

.inspect-copy {
  display: grid;
  gap: 8px;
}

.inspect-title,
.video-title {
  font-weight: 700;
}

.video-cell {
  display: grid;
  gap: 12px;
  grid-template-columns: 120px 1fr;
  align-items: center;
}

.muted {
  color: #6f89a4;
}

.error-copy {
  color: #d4380d;
}

.desktop-table {
  display: block;
}

.mobile-list {
  display: none;
}

.mobile-card {
  display: grid;
  gap: 14px;
  padding: 16px;
  border: 1px solid rgba(22, 32, 51, 0.08);
  border-radius: 18px;
  background: rgba(255, 255, 255, 0.72);
}

.mobile-card + .mobile-card {
  margin-top: 12px;
}

.mobile-card-top {
  display: grid;
  grid-template-columns: 96px 1fr;
  gap: 12px;
  align-items: start;
}

.mobile-copy {
  display: grid;
  gap: 6px;
  min-width: 0;
}

.mobile-status {
  color: #1f5f99;
  font-weight: 600;
}

.mobile-meta {
  display: grid;
  gap: 6px;
  color: #58708f;
  font-size: 13px;
}

.mobile-empty {
  padding: 24px 16px;
  text-align: center;
  color: #6f89a4;
}

.mobile-pagination {
  margin-top: 14px;
  text-align: center;
}

@media (max-width: 960px) {
  .hero {
    grid-template-columns: 1fr;
  }

  .hero h1 {
    max-width: none;
    font-size: clamp(26px, 9vw, 38px);
  }

  .inspect-layout :deep(.ant-space-item:first-child) {
    width: 100%;
  }

  .inspect-layout {
    display: grid;
  }

  .desktop-table {
    display: none;
  }

  .mobile-list {
    display: block;
  }

  .page {
    gap: 16px;
  }

  .video-cell {
    grid-template-columns: 1fr;
  }

  .queue-card :deep(.ant-space) {
    width: 100%;
  }

  .queue-card :deep(.ant-space .ant-space-item) {
    width: 100%;
  }

  .queue-card :deep(.ant-btn) {
    width: 100%;
  }
}
</style>
