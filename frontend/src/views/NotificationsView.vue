<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { message } from 'ant-design-vue'
import { ReloadOutlined, SendOutlined } from '@ant-design/icons-vue'

import {
  listNotificationRules,
  listNotifications,
  testDiskTemperatureNotificationRule,
  updateDiskTemperatureNotificationRule,
  type NotificationRecordDTO,
  type NotificationRuleDTO,
  type PagedNotifications,
} from '@/api/client'

const loading = ref(false)
const saving = ref(false)
const testing = ref(false)
const historyLoading = ref(false)
const rules = ref<NotificationRuleDTO[]>([])
const history = ref<PagedNotifications>({ items: [], total: 0, page: 1, pageSize: 20 })
const form = reactive({
  enabled: false,
  thresholdCelsius: 50,
  barkEnabled: false,
  barkServerUrl: 'https://api.day.app',
  barkDeviceKey: '',
})

const diskTemperatureRule = computed(() => rules.value.find((rule) => rule.id === 'disk-temperature') ?? null)
const barkConfigured = computed(() => form.barkDeviceKey.trim() !== '')

const ruleColumns = [
  { title: '规则', key: 'rule' },
  { title: '状态', key: 'enabled', width: 110 },
  { title: '告警温度', key: 'threshold', width: 120 },
  { title: '通知渠道', key: 'channel', width: 160 },
  { title: '冷却时间', key: 'cooldown', width: 110 },
  { title: '更新时间', key: 'updatedAt', width: 180 },
]

const historyColumns = [
  { title: '通知内容', key: 'content' },
  { title: '磁盘', key: 'disk', width: 220 },
  { title: '温度', key: 'temperature', width: 120 },
  { title: '渠道', key: 'channel', width: 90 },
  { title: '状态', key: 'status', width: 110 },
  { title: '抑制', key: 'suppressed', width: 90 },
  { title: '时间', key: 'createdAt', width: 180 },
]

async function load() {
  loading.value = true
  try {
    const [nextRules] = await Promise.all([listNotificationRules(), loadHistory()])
    rules.value = nextRules
    restoreForm()
  } catch (error) {
    message.error(error instanceof Error ? error.message : '加载通知配置失败')
  } finally {
    loading.value = false
  }
}

async function loadHistory(page = history.value.page, pageSize = history.value.pageSize) {
  historyLoading.value = true
  try {
    history.value = await listNotifications(page, pageSize)
  } catch (error) {
    message.error(error instanceof Error ? error.message : '加载通知历史失败')
  } finally {
    historyLoading.value = false
  }
}

function restoreForm() {
  const rule = diskTemperatureRule.value
  if (!rule) return
  form.enabled = Boolean(rule.enabled)
  form.thresholdCelsius = rule.thresholdCelsius || 50
  form.barkEnabled = Boolean(rule.barkEnabled)
  form.barkServerUrl = rule.barkServerUrl || 'https://api.day.app'
  form.barkDeviceKey = rule.barkDeviceKey || ''
}

async function saveRule() {
  saving.value = true
  try {
    const updated = await updateDiskTemperatureNotificationRule({
      enabled: form.enabled,
      thresholdCelsius: form.thresholdCelsius,
      barkEnabled: barkConfigured.value && form.barkEnabled,
      barkServerUrl: form.barkServerUrl,
      barkDeviceKey: form.barkDeviceKey.trim() || undefined,
    })
    const index = rules.value.findIndex((rule) => rule.id === updated.id)
    if (index >= 0) {
      rules.value[index] = updated
    } else {
      rules.value = [updated, ...rules.value]
    }
    restoreForm()
    message.success('通知规则已保存')
  } catch (error) {
    message.error(error instanceof Error ? error.message : '保存通知规则失败')
  } finally {
    saving.value = false
  }
}

async function testRule() {
  testing.value = true
  try {
    await testDiskTemperatureNotificationRule()
    message.success('测试通知已发送')
  } catch (error) {
    message.error(error instanceof Error ? error.message : '测试通知发送失败')
  } finally {
    testing.value = false
  }
}

function formatDate(value?: string) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

function ruleStatusColor(enabled: boolean) {
  return enabled ? 'processing' : 'default'
}

function sendStatusColor(status: string) {
  switch (status) {
    case 'sent':
      return 'success'
    case 'failed':
      return 'error'
    case 'disabled':
      return 'default'
    default:
      return 'warning'
  }
}

function sendStatusText(status: string) {
  switch (status) {
    case 'sent':
      return '已发送'
    case 'failed':
      return '发送失败'
    case 'disabled':
      return '未发送'
    default:
      return status || '-'
  }
}

function channelText(channel: string) {
  return channel === 'bark' ? 'Bark' : channel || '-'
}

function ruleChannelText(rule: NotificationRuleDTO) {
  return rule.barkEnabled && rule.barkDeviceKeySet ? 'Bark 已启用' : '无通知渠道'
}

function ruleChannelColor(rule: NotificationRuleDTO) {
  return rule.barkEnabled && rule.barkDeviceKeySet ? 'success' : 'default'
}

function diskName(record: Pick<NotificationRecordDTO, 'friendlyName' | 'serialNumber' | 'deviceId'>) {
  return record.friendlyName || record.serialNumber || record.deviceId || '-'
}

watch(
  () => form.barkDeviceKey,
  (value) => {
    if (!value.trim()) {
      form.barkEnabled = false
    }
  },
)

onMounted(load)
</script>

<template>
  <div class="page">
    <section class="page-header">
      <div>
        <div class="header-kicker">Notifications</div>
        <h1>通知配置</h1>
      </div>
      <a-space>
        <a-button :loading="historyLoading" @click="loadHistory()">
          <template #icon><ReloadOutlined /></template>
          刷新历史
        </a-button>
      </a-space>
    </section>

    <a-card :loading="loading" :bordered="false" class="panel">
      <div class="section-header">
        <div>
          <h2>Bark 服务配置</h2>
          <div class="field-hint">配置 Bark 服务端地址和 device key，保存后会在这里继续显示。</div>
        </div>
        <a-space>
          <a-button :loading="testing" :disabled="saving || !diskTemperatureRule?.barkDeviceKeySet" @click="testRule">
            <template #icon><SendOutlined /></template>
            测试 Bark
          </a-button>
          <a-button type="primary" :loading="saving" @click="saveRule">保存配置</a-button>
        </a-space>
      </div>

      <div class="rule-form">
        <a-form layout="vertical">
          <a-row :gutter="16">
            <a-col :xs="24" :md="12">
              <a-form-item label="Bark 服务端">
                <a-input v-model:value="form.barkServerUrl" placeholder="https://api.day.app" />
              </a-form-item>
            </a-col>
            <a-col :xs="24" :md="12">
              <a-form-item label="Bark Device Key">
                <a-input v-model:value="form.barkDeviceKey" placeholder="请输入 Bark device key" />
              </a-form-item>
            </a-col>
          </a-row>
        </a-form>
      </div>

      <div class="rule-form">
        <div class="section-subtitle">
          <h2>通知规则</h2>
          <div class="field-hint">当前支持磁盘温度告警；同一块磁盘 1 小时内不会重复推送。</div>
        </div>
        <a-form layout="vertical">
          <a-row :gutter="16">
            <a-col :xs="24" :md="8">
              <a-form-item label="磁盘温度告警">
                <a-switch v-model:checked="form.enabled" checked-children="开" un-checked-children="关" />
              </a-form-item>
            </a-col>
            <a-col :xs="24" :md="8">
              <a-form-item label="告警温度">
                <a-input-number v-model:value="form.thresholdCelsius" style="width: 100%" :min="1" :max="120" addon-after="°C" />
              </a-form-item>
            </a-col>
            <a-col :xs="24" :md="8">
              <a-form-item label="通知渠道">
                <a-checkbox v-model:checked="form.barkEnabled" :disabled="!barkConfigured">Bark</a-checkbox>
                <div v-if="!barkConfigured" class="field-hint">未配置 Bark device key，告警只记录不通知。</div>
              </a-form-item>
            </a-col>
          </a-row>
        </a-form>
      </div>

      <a-table
        class="desktop-table rule-table"
        row-key="id"
        :columns="ruleColumns"
        :data-source="rules"
        :pagination="false"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'rule'">
            <div class="rule-title">{{ record.name }}</div>
            <div class="muted">{{ record.type }}</div>
          </template>
          <template v-else-if="column.key === 'enabled'">
            <a-tag :color="ruleStatusColor(record.enabled)">{{ record.enabled ? '已启用' : '未启用' }}</a-tag>
          </template>
          <template v-else-if="column.key === 'threshold'">
            {{ record.thresholdCelsius }}°C
          </template>
          <template v-else-if="column.key === 'channel'">
            <a-tag :color="ruleChannelColor(record)">{{ ruleChannelText(record) }}</a-tag>
          </template>
          <template v-else-if="column.key === 'cooldown'">
            {{ record.cooldownMinutes }} 分钟
          </template>
          <template v-else-if="column.key === 'updatedAt'">
            {{ formatDate(record.updatedAt) }}
          </template>
        </template>
      </a-table>
    </a-card>

    <a-card :bordered="false" class="panel">
      <div class="section-header">
        <div>
          <h2>通知历史</h2>
          <div class="field-hint">记录磁盘温度告警的 Bark 发送结果和冷却抑制次数。</div>
        </div>
      </div>

      <a-table
        class="desktop-table"
        row-key="id"
        :columns="historyColumns"
        :data-source="history.items"
        :loading="historyLoading"
        :pagination="{ current: history.page, pageSize: history.pageSize, total: history.total, onChange: loadHistory }"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'content'">
            <div class="rule-title">{{ record.title }}</div>
            <div class="muted">{{ record.body }}</div>
            <div v-if="record.errorMessage" class="error-copy">{{ record.errorMessage }}</div>
          </template>
          <template v-else-if="column.key === 'disk'">
            <div>{{ diskName(record) }}</div>
            <div class="muted">{{ record.serialNumber || record.deviceId || '-' }}</div>
          </template>
          <template v-else-if="column.key === 'temperature'">
            {{ record.temperatureCelsius }}°C / {{ record.thresholdCelsius }}°C
          </template>
          <template v-else-if="column.key === 'channel'">
            {{ channelText(record.channel) }}
          </template>
          <template v-else-if="column.key === 'status'">
            <a-tag :color="sendStatusColor(record.status)">{{ sendStatusText(record.status) }}</a-tag>
          </template>
          <template v-else-if="column.key === 'suppressed'">
            {{ record.suppressedCount || 0 }}
          </template>
          <template v-else-if="column.key === 'createdAt'">
            {{ formatDate(record.createdAt) }}
          </template>
        </template>
      </a-table>

      <div class="mobile-list">
        <div v-if="history.items.length === 0" class="mobile-empty">暂无通知历史</div>
        <div v-for="record in history.items" :key="record.id" class="mobile-card">
          <div class="mobile-card-top">
            <div>
              <div class="rule-title">{{ record.title }}</div>
              <div class="muted">{{ formatDate(record.createdAt) }}</div>
            </div>
            <a-tag :color="sendStatusColor(record.status)">{{ sendStatusText(record.status) }}</a-tag>
          </div>
          <div class="mobile-meta">
            <span>磁盘：{{ diskName(record) }}</span>
            <span>温度：{{ record.temperatureCelsius }}°C / {{ record.thresholdCelsius }}°C</span>
            <span>渠道：{{ channelText(record.channel) }}</span>
            <span>抑制：{{ record.suppressedCount || 0 }}</span>
          </div>
          <div class="muted">{{ record.body }}</div>
          <div v-if="record.errorMessage" class="error-copy">{{ record.errorMessage }}</div>
        </div>
        <a-pagination
          v-if="history.total > history.pageSize"
          class="mobile-pagination"
          :current="history.page"
          :page-size="history.pageSize"
          :total="history.total"
          simple
          @change="loadHistory"
        />
      </div>
    </a-card>
  </div>
</template>

<style scoped>
.page {
  display: grid;
  gap: 24px;
}

.page-header,
.section-header {
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

h1,
h2 {
  margin: 0;
}

.panel {
  background: rgba(255, 255, 255, 0.82);
  border-radius: 18px;
}

.rule-form {
  margin-top: 20px;
  padding-top: 18px;
  border-top: 1px solid rgba(22, 32, 51, 0.08);
}

.section-subtitle {
  margin-bottom: 16px;
}

.rule-table {
  margin-top: 18px;
}

.rule-title {
  font-weight: 700;
}

.field-hint,
.muted {
  color: #6f89a4;
  font-size: 12px;
}

.error-copy {
  color: #cf1322;
  font-size: 12px;
  margin-top: 4px;
  word-break: break-all;
}

.mobile-list {
  display: none;
}

@media (max-width: 760px) {
  .page-header,
  .section-header {
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
    gap: 10px;
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

  .mobile-pagination {
    justify-self: center;
  }
}
</style>
