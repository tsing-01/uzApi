<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <div class="flex-1 sm:max-w-64">
            <input
              v-model="searchQuery"
              type="text"
              :placeholder="t('admin.activation.searchCodes')"
              class="input"
              @input="handleSearch"
            />
          </div>
          <Select
            v-model="filters.status"
            :options="filterStatusOptions"
            class="w-36"
            @change="loadCodes"
          />

          <div class="flex flex-1 flex-wrap items-center justify-end gap-2">
            <button
              @click="loadCodes"
              :disabled="loading"
              class="btn btn-secondary"
              :title="t('common.refresh')"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <button @click="showCreateDialog = true" class="btn btn-primary">
              <Icon name="plus" size="md" class="mr-1" />
              {{ t('admin.activation.createCode') }}
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable
          :columns="columns"
          :data="codes"
          :loading="loading"
          :server-side-sort="true"
          default-sort-key="created_at"
          default-sort-order="desc"
          @sort="handleSort"
        >
          <template #cell-code="{ value }">
            <div class="flex items-center space-x-2">
              <code class="font-mono text-sm text-gray-900 dark:text-gray-100">{{ value }}</code>
              <button
                @click="copyToClipboard(value)"
                :class="[
                  'flex items-center transition-colors',
                  copiedCode === value
                    ? 'text-green-500'
                    : 'text-gray-400 hover:text-gray-600 dark:hover:text-gray-300'
                ]"
                :title="copiedCode === value ? t('admin.activation.copied') : t('keys.copyToClipboard')"
              >
                <Icon v-if="copiedCode !== value" name="copy" size="sm" :stroke-width="2" />
                <svg v-else class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M5 13l4 4L19 7"
                  />
                </svg>
              </button>
            </div>
          </template>

          <template #cell-amount="{ value }">
            <span class="text-sm font-medium text-gray-900 dark:text-white">
              ¥{{ value.toFixed(2) }}
            </span>
          </template>

          <template #cell-status="{ row }">
            <span :class="['badge', getStatusClass(row)]">{{ getStatusLabel(row) }}</span>
          </template>

          <template #cell-starts_at="{ value }">
            <span class="text-sm text-gray-500 dark:text-dark-400">
              {{ value ? formatDateTime(value) : t('admin.activation.startsImmediately') }}
            </span>
          </template>

          <template #cell-expires_at="{ value }">
            <span class="text-sm text-gray-500 dark:text-dark-400">
              {{ value ? formatDateTime(value) : t('admin.activation.neverExpires') }}
            </span>
          </template>

          <template #cell-used_by="{ row }">
            <span v-if="row.used_by" class="text-sm text-gray-700 dark:text-gray-200">
              {{ t('admin.activation.userPrefix', { id: row.used_by }) }}
              <span class="ml-1 block text-xs text-gray-400">
                {{ row.used_at ? formatDateTime(row.used_at) : '' }}
              </span>
            </span>
            <span v-else class="text-sm text-gray-400">—</span>
          </template>

          <template #cell-created_at="{ value }">
            <span class="text-sm text-gray-500 dark:text-dark-400">
              {{ formatDateTime(value) }}
            </span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center space-x-1">
              <button
                v-if="row.used_by"
                @click="handleViewLoginIPs(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-blue-50 hover:text-blue-600 dark:hover:bg-blue-900/20 dark:hover:text-blue-400"
                :title="t('admin.activation.loginIPs')"
              >
                <Icon name="globe" size="sm" />
              </button>
              <button
                @click="handleEdit(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-600 dark:hover:text-gray-300"
                :title="t('common.edit')"
              >
                <Icon name="edit" size="sm" />
              </button>
              <button
                @click="handleDelete(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
                :title="t('common.delete')"
              >
                <Icon name="trash" size="sm" />
              </button>
            </div>
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>

    <!-- Create Dialog -->
    <BaseDialog
      :show="showCreateDialog"
      :title="t('admin.activation.createCode')"
      width="normal"
      @close="showCreateDialog = false"
    >
      <form id="create-activation-form" @submit.prevent="handleCreate" class="space-y-4">
        <div>
          <label class="input-label">
            {{ t('admin.activation.code') }}
            <span class="ml-1 text-xs font-normal text-gray-400">
              ({{ t('admin.activation.autoGenerate') }})
            </span>
          </label>
          <input
            v-model="createForm.code"
            type="text"
            class="input font-mono uppercase"
            :placeholder="t('admin.activation.codePlaceholder')"
            :disabled="createForm.count > 1"
          />
        </div>
        <div>
          <label class="input-label">{{ t('admin.activation.amount') }}</label>
          <input
            v-model.number="createForm.amount"
            type="number"
            step="0.01"
            min="0"
            required
            class="input"
          />
        </div>
        <div>
          <label class="input-label">
            {{ t('admin.activation.startsAt') }}
            <span class="ml-1 text-xs font-normal text-gray-400">
              ({{ t('admin.activation.startsAtHint') }})
            </span>
          </label>
          <input v-model="createForm.starts_at_str" type="datetime-local" class="input" />
        </div>
        <div>
          <label class="input-label">
            {{ t('admin.activation.expiresAt') }}
            <span class="ml-1 text-xs font-normal text-gray-400">({{ t('common.optional') }})</span>
          </label>
          <input v-model="createForm.expires_at_str" type="datetime-local" class="input" />
        </div>
        <div>
          <label class="input-label">
            {{ t('admin.activation.count') }}
            <span class="ml-1 text-xs font-normal text-gray-400">
              ({{ t('admin.activation.countHint') }})
            </span>
          </label>
          <input v-model.number="createForm.count" type="number" min="1" max="200" class="input" />
        </div>
        <div>
          <label class="input-label">
            {{ t('admin.activation.notes') }}
            <span class="ml-1 text-xs font-normal text-gray-400">({{ t('common.optional') }})</span>
          </label>
          <textarea
            v-model="createForm.notes"
            rows="2"
            class="input"
            :placeholder="t('admin.activation.notesPlaceholder')"
          ></textarea>
        </div>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" @click="showCreateDialog = false" class="btn btn-secondary">
            {{ t('common.cancel') }}
          </button>
          <button
            type="submit"
            form="create-activation-form"
            :disabled="creating"
            class="btn btn-primary"
          >
            {{ creating ? t('common.creating') : t('common.create') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Edit Dialog -->
    <BaseDialog
      :show="showEditDialog"
      :title="t('admin.activation.editCode')"
      width="normal"
      @close="closeEditDialog"
    >
      <form id="edit-activation-form" @submit.prevent="handleUpdate" class="space-y-4">
        <div>
          <label class="input-label">{{ t('admin.activation.code') }}</label>
          <input
            :value="editingCode?.code"
            type="text"
            class="input font-mono uppercase"
            disabled
          />
        </div>
        <div>
          <label class="input-label">
            {{ t('admin.activation.amount') }}
            <span
              v-if="editingCode && editingCode.status !== 'unused'"
              class="ml-1 text-xs font-normal text-gray-400"
            >
              ({{ t('admin.activation.amountLocked') }})
            </span>
          </label>
          <input
            v-model.number="editForm.amount"
            type="number"
            step="0.01"
            min="0"
            class="input"
            :disabled="!!editingCode && editingCode.status !== 'unused'"
          />
        </div>
        <div v-if="editableStatus">
          <label class="input-label">{{ t('admin.activation.status') }}</label>
          <Select v-model="editForm.status" :options="statusOptions" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.activation.startsAt') }}</label>
          <input v-model="editForm.starts_at_str" type="datetime-local" class="input" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.activation.expiresAt') }}</label>
          <input v-model="editForm.expires_at_str" type="datetime-local" class="input" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.activation.notes') }}</label>
          <textarea v-model="editForm.notes" rows="2" class="input"></textarea>
        </div>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" @click="closeEditDialog" class="btn btn-secondary">
            {{ t('common.cancel') }}
          </button>
          <button
            type="submit"
            form="edit-activation-form"
            :disabled="updating"
            class="btn btn-primary"
          >
            {{ updating ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Generated Codes Dialog -->
    <BaseDialog
      :show="showResultDialog"
      :title="t('admin.activation.generatedSuccessfully')"
      width="normal"
      @close="closeResultDialog"
    >
      <div class="space-y-3">
        <p class="text-sm text-gray-500 dark:text-gray-400">
          {{ t('admin.activation.codesCreated', { count: generatedCodes.length }) }}
        </p>
        <textarea
          readonly
          :value="generatedCodesText"
          rows="8"
          class="w-full resize-none rounded-lg border border-gray-200 bg-gray-50 p-3 font-mono text-sm text-gray-800 focus:outline-none dark:border-dark-600 dark:bg-dark-700 dark:text-gray-200"
        ></textarea>
      </div>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button @click="copyGeneratedCodes" class="btn btn-secondary">
            {{ copiedAll ? t('admin.activation.copied') : t('admin.activation.copyAll') }}
          </button>
          <button @click="closeResultDialog" class="btn btn-primary">
            {{ t('common.close') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Login IPs Dialog -->
    <BaseDialog
      :show="showLoginIPsDialog"
      :title="t('admin.activation.loginIPs')"
      width="normal"
      @close="showLoginIPsDialog = false"
    >
      <div v-if="loginIPsLoading" class="flex items-center justify-center py-8">
        <Icon name="refresh" size="lg" class="animate-spin text-gray-400" />
      </div>
      <div v-else class="space-y-3">
        <p class="text-sm text-gray-500 dark:text-gray-400">
          {{
            t('admin.activation.loginIPsUsage', {
              used: loginIPs.length,
              limit: loginIPLimit
            })
          }}
        </p>
        <div
          v-if="loginIPs.length === 0"
          class="py-6 text-center text-gray-500 dark:text-gray-400"
        >
          {{ t('admin.activation.noLoginIPs') }}
        </div>
        <div
          v-for="entry in loginIPs"
          :key="entry.ip"
          class="flex items-center justify-between rounded-lg border border-gray-200 p-3 dark:border-dark-600"
        >
          <code class="font-mono text-sm text-gray-900 dark:text-gray-100">{{ entry.ip }}</code>
          <div class="text-right text-xs text-gray-500 dark:text-gray-400">
            <div>{{ t('admin.activation.firstSeen') }}: {{ formatDateTime(entry.created_at) }}</div>
            <div>{{ t('admin.activation.lastSeen') }}: {{ formatDateTime(entry.last_seen_at) }}</div>
          </div>
        </div>
      </div>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button
            @click="handleResetLoginIPs"
            :disabled="resettingIPs || loginIPs.length === 0"
            class="btn btn-secondary"
          >
            {{ resettingIPs ? t('common.saving') : t('admin.activation.resetLoginIPs') }}
          </button>
          <button @click="showLoginIPsDialog = false" class="btn btn-primary">
            {{ t('common.close') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Delete Confirmation -->
    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('admin.activation.deleteCode')"
      :message="t('admin.activation.deleteCodeConfirm')"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      danger
      @confirm="confirmDelete"
      @cancel="showDeleteDialog = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { adminAPI } from '@/api/admin'
import { formatDateTime } from '@/utils/format'
import type { ActivationCode, UserLoginIP } from '@/types'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard: clipboardCopy } = useClipboard()

// State
const codes = ref<ActivationCode[]>([])
const loading = ref(false)
const creating = ref(false)
const updating = ref(false)
const searchQuery = ref('')
const copiedCode = ref<string | null>(null)

const filters = reactive({
  status: ''
})

const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0
})

const sortState = reactive({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc'
})

// Dialogs
const showCreateDialog = ref(false)
const showEditDialog = ref(false)
const showDeleteDialog = ref(false)
const showResultDialog = ref(false)
const showLoginIPsDialog = ref(false)

const editingCode = ref<ActivationCode | null>(null)
const deletingCode = ref<ActivationCode | null>(null)

// Batch create result
const generatedCodes = ref<ActivationCode[]>([])
const copiedAll = ref(false)

// Login IPs
const loginIPs = ref<UserLoginIP[]>([])
const loginIPLimit = ref(2)
const loginIPsLoading = ref(false)
const resettingIPs = ref(false)
const loginIPsUserId = ref<number | null>(null)

// Forms
const createForm = reactive({
  code: '',
  amount: 1,
  starts_at_str: '',
  expires_at_str: '',
  count: 1,
  notes: ''
})

const editForm = reactive({
  amount: 0,
  status: 'unused' as 'unused' | 'disabled',
  starts_at_str: '',
  expires_at_str: '',
  notes: ''
})

// 已绑定/被替换的码由绑定流程维护状态，后端只接受 unused / disabled
const editableStatus = computed(
  () => !editingCode.value || ['unused', 'disabled'].includes(editingCode.value.status)
)

const filterStatusOptions = computed(() => [
  { value: '', label: t('admin.activation.allStatus') },
  { value: 'unused', label: t('admin.activation.statusUnused') },
  { value: 'used', label: t('admin.activation.statusUsed') },
  { value: 'replaced', label: t('admin.activation.statusReplaced') },
  { value: 'disabled', label: t('admin.activation.statusDisabled') }
])

const statusOptions = computed(() => [
  { value: 'unused', label: t('admin.activation.statusUnused') },
  { value: 'disabled', label: t('admin.activation.statusDisabled') }
])

const columns = computed<Column[]>(() => [
  { key: 'code', label: t('admin.activation.columns.code') },
  { key: 'amount', label: t('admin.activation.columns.amount'), sortable: true },
  { key: 'status', label: t('admin.activation.columns.status'), sortable: true },
  { key: 'starts_at', label: t('admin.activation.columns.startsAt'), sortable: true },
  { key: 'expires_at', label: t('admin.activation.columns.expiresAt'), sortable: true },
  { key: 'used_by', label: t('admin.activation.columns.usedBy') },
  { key: 'created_at', label: t('admin.activation.columns.createdAt'), sortable: true },
  { key: 'actions', label: t('admin.activation.columns.actions') }
])

const generatedCodesText = computed(() => generatedCodes.value.map((c) => c.code).join('\n'))

// Helpers
const isExpired = (row: ActivationCode) =>
  !!row.expires_at && new Date(row.expires_at).getTime() <= Date.now()
const isNotStarted = (row: ActivationCode) =>
  !!row.starts_at && new Date(row.starts_at).getTime() > Date.now()

const getStatusClass = (row: ActivationCode) => {
  if (row.status === 'disabled') return 'badge-gray'
  if (row.status === 'replaced') return 'badge-gray'
  if (isExpired(row)) return 'badge-danger'
  if (isNotStarted(row)) return 'badge-warning'
  return row.status === 'used' ? 'badge-primary' : 'badge-success'
}

const getStatusLabel = (row: ActivationCode) => {
  if (row.status === 'disabled') return t('admin.activation.statusDisabled')
  if (row.status === 'replaced') return t('admin.activation.statusReplaced')
  if (isExpired(row)) return t('admin.activation.statusExpired')
  if (isNotStarted(row)) return t('admin.activation.statusNotStarted')
  return row.status === 'used'
    ? t('admin.activation.statusUsed')
    : t('admin.activation.statusUnused')
}

const toTimestamp = (value: string): number | undefined =>
  value ? Math.floor(new Date(value).getTime() / 1000) : undefined

const toLocalInput = (value: string | null): string => {
  if (!value) return ''
  const date = new Date(value)
  const offset = date.getTimezoneOffset() * 60000
  return new Date(date.getTime() - offset).toISOString().slice(0, 16)
}

// API calls
let abortController: AbortController | null = null

const loadCodes = async () => {
  if (abortController) {
    abortController.abort()
  }
  const currentController = new AbortController()
  abortController = currentController
  loading.value = true

  try {
    const response = await adminAPI.activationCodes.list(
      pagination.page,
      pagination.page_size,
      {
        status: filters.status || undefined,
        search: searchQuery.value || undefined,
        sort_by: sortState.sort_by,
        sort_order: sortState.sort_order
      },
      { signal: currentController.signal }
    )
    if (currentController.signal.aborted || abortController !== currentController) return

    codes.value = response.items
    pagination.total = response.total
  } catch (error: any) {
    if (
      currentController.signal.aborted ||
      abortController !== currentController ||
      error?.name === 'AbortError' ||
      error?.code === 'ERR_CANCELED'
    ) {
      return
    }
    appStore.showError(t('admin.activation.failedToLoad'))
    console.error('Error loading activation codes:', error)
  } finally {
    if (abortController === currentController) {
      loading.value = false
      abortController = null
    }
  }
}

let searchTimeout: ReturnType<typeof setTimeout>
const handleSearch = () => {
  clearTimeout(searchTimeout)
  searchTimeout = setTimeout(() => {
    pagination.page = 1
    loadCodes()
  }, 300)
}

const handlePageChange = (page: number) => {
  pagination.page = page
  loadCodes()
}

const handlePageSizeChange = (pageSize: number) => {
  pagination.page_size = pageSize
  pagination.page = 1
  loadCodes()
}

const handleSort = (key: string, order: 'asc' | 'desc') => {
  sortState.sort_by = key
  sortState.sort_order = order
  pagination.page = 1
  loadCodes()
}

const copyToClipboard = async (text: string) => {
  const success = await clipboardCopy(text, t('admin.activation.copied'))
  if (success) {
    copiedCode.value = text
    setTimeout(() => {
      copiedCode.value = null
    }, 2000)
  }
}

// Create
const handleCreate = async () => {
  const startsAt = toTimestamp(createForm.starts_at_str)
  const expiresAt = toTimestamp(createForm.expires_at_str)
  if (startsAt && expiresAt && expiresAt <= startsAt) {
    appStore.showError(t('admin.activation.invalidPeriod'))
    return
  }

  creating.value = true
  try {
    const created = await adminAPI.activationCodes.create({
      code: createForm.count > 1 ? undefined : createForm.code || undefined,
      amount: createForm.amount,
      starts_at: startsAt,
      expires_at: expiresAt,
      count: createForm.count > 1 ? createForm.count : undefined,
      notes: createForm.notes || undefined
    })
    appStore.showSuccess(t('admin.activation.codeCreated'))
    showCreateDialog.value = false
    resetCreateForm()
    generatedCodes.value = created
    if (created.length > 0) {
      showResultDialog.value = true
    }
    loadCodes()
  } catch (error: any) {
    appStore.showError(
      error.response?.data?.message || t('admin.activation.failedToCreate')
    )
  } finally {
    creating.value = false
  }
}

const resetCreateForm = () => {
  createForm.code = ''
  createForm.amount = 1
  createForm.starts_at_str = ''
  createForm.expires_at_str = ''
  createForm.count = 1
  createForm.notes = ''
}

const closeResultDialog = () => {
  showResultDialog.value = false
  generatedCodes.value = []
  copiedAll.value = false
}

const copyGeneratedCodes = async () => {
  const success = await clipboardCopy(generatedCodesText.value, t('admin.activation.copied'))
  if (success) {
    copiedAll.value = true
    setTimeout(() => {
      copiedAll.value = false
    }, 2000)
  }
}

// Edit
const handleEdit = (code: ActivationCode) => {
  editingCode.value = code
  editForm.amount = code.amount
  editForm.status = code.status === 'disabled' ? 'disabled' : 'unused'
  editForm.starts_at_str = toLocalInput(code.starts_at)
  editForm.expires_at_str = toLocalInput(code.expires_at)
  editForm.notes = code.notes || ''
  showEditDialog.value = true
}

const closeEditDialog = () => {
  showEditDialog.value = false
  editingCode.value = null
}

const handleUpdate = async () => {
  if (!editingCode.value) return

  const startsAt = toTimestamp(editForm.starts_at_str)
  const expiresAt = toTimestamp(editForm.expires_at_str)
  if (startsAt && expiresAt && expiresAt <= startsAt) {
    appStore.showError(t('admin.activation.invalidPeriod'))
    return
  }

  updating.value = true
  try {
    await adminAPI.activationCodes.update(editingCode.value.id, {
      // 已绑定的码后端不接受改金额
      amount: editingCode.value.status === 'unused' ? editForm.amount : undefined,
      status: editableStatus.value ? editForm.status : undefined,
      // 传 0 表示清空该时间
      starts_at: startsAt ?? 0,
      expires_at: expiresAt ?? 0,
      notes: editForm.notes
    })
    appStore.showSuccess(t('admin.activation.codeUpdated'))
    closeEditDialog()
    loadCodes()
  } catch (error: any) {
    appStore.showError(
      error.response?.data?.message || t('admin.activation.failedToUpdate')
    )
  } finally {
    updating.value = false
  }
}

// Delete
const handleDelete = (code: ActivationCode) => {
  deletingCode.value = code
  showDeleteDialog.value = true
}

const confirmDelete = async () => {
  if (!deletingCode.value) return

  try {
    await adminAPI.activationCodes.delete(deletingCode.value.id)
    appStore.showSuccess(t('admin.activation.codeDeleted'))
    showDeleteDialog.value = false
    deletingCode.value = null
    loadCodes()
  } catch (error: any) {
    appStore.showError(
      error.response?.data?.message || t('admin.activation.failedToDelete')
    )
  }
}

// Login IPs
const handleViewLoginIPs = async (code: ActivationCode) => {
  if (!code.used_by) return
  loginIPsUserId.value = code.used_by
  showLoginIPsDialog.value = true
  await loadLoginIPs()
}

const loadLoginIPs = async () => {
  if (!loginIPsUserId.value) return
  loginIPsLoading.value = true
  try {
    const response = await adminAPI.activationCodes.getUserLoginIPs(loginIPsUserId.value)
    loginIPs.value = response.login_ips || []
    loginIPLimit.value = response.login_ip_limit
  } catch (error: any) {
    appStore.showError(
      error.response?.data?.message || t('admin.activation.failedToLoadLoginIPs')
    )
  } finally {
    loginIPsLoading.value = false
  }
}

const handleResetLoginIPs = async () => {
  if (!loginIPsUserId.value) return
  resettingIPs.value = true
  try {
    await adminAPI.activationCodes.resetUserLoginIPs(loginIPsUserId.value)
    appStore.showSuccess(t('admin.activation.loginIPsReset'))
    await loadLoginIPs()
  } catch (error: any) {
    appStore.showError(
      error.response?.data?.message || t('admin.activation.failedToResetLoginIPs')
    )
  } finally {
    resettingIPs.value = false
  }
}

onMounted(() => {
  loadCodes()
})

onUnmounted(() => {
  clearTimeout(searchTimeout)
  abortController?.abort()
})
</script>
