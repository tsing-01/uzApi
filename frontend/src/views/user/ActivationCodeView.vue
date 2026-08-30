<template>
  <AppLayout>
    <div class="mx-auto max-w-2xl space-y-6">
      <!-- Current Activation Status -->
      <div class="card overflow-hidden">
        <div
          :class="[
            'px-6 py-8 text-center',
            status?.valid
              ? 'bg-gradient-to-br from-primary-500 to-primary-600'
              : 'bg-gradient-to-br from-gray-400 to-gray-500 dark:from-dark-600 dark:to-dark-700'
          ]"
        >
          <div
            class="mb-4 inline-flex h-16 w-16 items-center justify-center rounded-2xl bg-white/20 backdrop-blur-sm"
          >
            <Icon :name="status?.valid ? 'checkCircle' : 'key'" size="xl" class="text-white" />
          </div>
          <p class="text-sm font-medium text-white/80">{{ t('activation.currentStatus') }}</p>
          <p class="mt-2 text-2xl font-bold text-white">
            {{ statusLabel }}
          </p>
          <p v-if="status?.code" class="mt-2 font-mono text-sm text-white/80">
            {{ status.code.code }}
          </p>
        </div>

        <div v-if="status?.bound && status.code" class="divide-y divide-gray-100 dark:divide-dark-700">
          <div class="flex items-center justify-between px-6 py-3 text-sm">
            <span class="text-gray-500 dark:text-dark-400">{{ t('activation.amount') }}</span>
            <span class="font-medium text-gray-900 dark:text-white">
              ¥{{ status.code.amount.toFixed(2) }}
            </span>
          </div>
          <div class="flex items-center justify-between px-6 py-3 text-sm">
            <span class="text-gray-500 dark:text-dark-400">{{ t('activation.startsAt') }}</span>
            <span class="text-gray-900 dark:text-white">
              {{
                status.code.starts_at
                  ? formatDateTime(status.code.starts_at)
                  : t('activation.startsImmediately')
              }}
            </span>
          </div>
          <div class="flex items-center justify-between px-6 py-3 text-sm">
            <span class="text-gray-500 dark:text-dark-400">{{ t('activation.expiresAt') }}</span>
            <span class="text-gray-900 dark:text-white">
              {{
                status.code.expires_at
                  ? formatDateTime(status.code.expires_at)
                  : t('activation.neverExpires')
              }}
            </span>
          </div>
          <div class="flex items-center justify-between px-6 py-3 text-sm">
            <span class="text-gray-500 dark:text-dark-400">{{ t('activation.boundAt') }}</span>
            <span class="text-gray-900 dark:text-white">
              {{ status.code.used_at ? formatDateTime(status.code.used_at) : '—' }}
            </span>
          </div>
        </div>
      </div>

      <!-- Login IPs -->
      <div v-if="status && status.login_ip_limit > 0" class="card">
        <div class="p-6">
          <div class="flex items-center justify-between">
            <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('activation.loginIPs') }}
            </h3>
            <span class="text-sm text-gray-500 dark:text-dark-400">
              {{ t('activation.loginIPsUsage', {
                used: status.login_ip_used,
                limit: status.login_ip_limit
              }) }}
            </span>
          </div>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
            {{ t('activation.loginIPsHint', { limit: status.login_ip_limit }) }}
          </p>
          <div v-if="status.login_ips.length > 0" class="mt-4 space-y-2">
            <div
              v-for="entry in status.login_ips"
              :key="entry.ip"
              class="flex items-center justify-between rounded-lg border border-gray-200 px-3 py-2 dark:border-dark-600"
            >
              <code class="font-mono text-sm text-gray-900 dark:text-gray-100">{{ entry.ip }}</code>
              <span class="text-xs text-gray-500 dark:text-dark-400">
                {{ t('activation.lastSeen') }}: {{ formatDateTime(entry.last_seen_at) }}
              </span>
            </div>
          </div>
          <p v-else class="mt-4 text-sm text-gray-500 dark:text-dark-400">
            {{ t('activation.noLoginIPs') }}
          </p>
        </div>
      </div>

      <!-- Add Activation Code -->
      <div class="card">
        <div class="p-6">
          <form @submit.prevent="handleAdd" class="space-y-5">
            <div>
              <label for="activation-code" class="input-label">
                {{ t('activation.codeLabel') }}
              </label>
              <div class="relative mt-1">
                <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-4">
                  <Icon name="key" size="md" class="text-gray-400 dark:text-dark-500" />
                </div>
                <input
                  id="activation-code"
                  v-model="activationCode"
                  type="text"
                  required
                  :placeholder="t('activation.codePlaceholder')"
                  :disabled="submitting"
                  class="input py-3 pl-12 text-lg"
                />
              </div>
              <p class="input-hint">{{ t('activation.codeHint') }}</p>
            </div>

            <button
              type="submit"
              :disabled="!activationCode || submitting"
              class="btn btn-primary w-full py-3"
            >
              <Icon
                :name="submitting ? 'refresh' : 'checkCircle'"
                size="md"
                :class="['mr-2', submitting ? 'animate-spin' : '']"
              />
              {{ submitting ? t('activation.submitting') : t('activation.submit') }}
            </button>
          </form>
        </div>
      </div>

      <!-- Success -->
      <transition name="fade">
        <div
          v-if="addResult"
          class="card border-emerald-200 bg-emerald-50 dark:border-emerald-800/50 dark:bg-emerald-900/20"
        >
          <div class="p-6">
            <div class="flex items-start gap-4">
              <div
                class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-xl bg-emerald-100 dark:bg-emerald-900/30"
              >
                <Icon name="checkCircle" size="md" class="text-emerald-600 dark:text-emerald-400" />
              </div>
              <div class="flex-1 text-sm text-emerald-700 dark:text-emerald-400">
                <h3 class="text-sm font-semibold text-emerald-800 dark:text-emerald-300">
                  {{ t('activation.addSuccess') }}
                </h3>
                <div class="mt-2 space-y-1">
                  <p class="font-medium">
                    {{ t('activation.added') }}: ¥{{ addResult.amount.toFixed(2) }}
                  </p>
                  <p>
                    {{ t('activation.newBalance') }}:
                    <span class="font-semibold">¥{{ addResult.new_balance.toFixed(2) }}</span>
                  </p>
                  <p v-if="addResult.replaced > 0">{{ t('activation.previousReplaced') }}</p>
                  <p v-if="addResult.login_ip_reset">{{ t('activation.loginIPsResetNotice') }}</p>
                </div>
              </div>
            </div>
          </div>
        </div>
      </transition>

      <!-- Error -->
      <transition name="fade">
        <div
          v-if="errorMessage"
          class="card border-red-200 bg-red-50 dark:border-red-800/50 dark:bg-red-900/20"
        >
          <div class="p-6">
            <div class="flex items-start gap-4">
              <div
                class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-xl bg-red-100 dark:bg-red-900/30"
              >
                <Icon name="xCircle" size="md" class="text-red-600 dark:text-red-400" />
              </div>
              <div class="flex-1">
                <h3 class="text-sm font-semibold text-red-800 dark:text-red-300">
                  {{ t('activation.addFailed') }}
                </h3>
                <p class="mt-1 text-sm text-red-700 dark:text-red-400">{{ errorMessage }}</p>
              </div>
            </div>
          </div>
        </div>
      </transition>

      <!-- About -->
      <div
        class="rounded-xl border border-primary-100 bg-primary-50/60 p-6 dark:border-primary-900/40 dark:bg-primary-900/10"
      >
        <div class="flex items-start gap-4">
          <div
            class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-xl bg-primary-100 dark:bg-primary-900/30"
          >
            <Icon name="infoCircle" size="md" class="text-primary-600 dark:text-primary-400" />
          </div>
          <div>
            <h3 class="text-sm font-semibold text-primary-800 dark:text-primary-300">
              {{ t('activation.aboutTitle') }}
            </h3>
            <ul class="mt-2 list-disc space-y-1 pl-5 text-sm text-primary-700 dark:text-primary-400">
              <li>{{ t('activation.aboutOneUse') }}</li>
              <li>{{ t('activation.aboutCredit') }}</li>
              <li>{{ t('activation.aboutReplace') }}</li>
              <li>{{ t('activation.aboutLoginIP') }}</li>
            </ul>
          </div>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { activationCodeAPI } from '@/api'
import { formatDateTime } from '@/utils/format'
import type { ActivationStatus, AddActivationCodeResponse } from '@/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()

const status = ref<ActivationStatus | null>(null)
const activationCode = ref('')
const submitting = ref(false)
const addResult = ref<AddActivationCodeResponse | null>(null)
const errorMessage = ref('')

const statusLabel = computed(() => {
  if (!status.value) return t('common.loading')
  if (status.value.valid) return t('activation.statusValid')
  switch (status.value.reason) {
    case 'NOT_BOUND':
      return t('activation.statusNotBound')
    case 'NOT_STARTED':
      return t('activation.statusNotStarted')
    case 'EXPIRED':
      return t('activation.statusExpired')
    case 'DISABLED':
      return t('activation.statusDisabled')
    case 'REPLACED':
      return t('activation.statusReplaced')
    default:
      return t('activation.statusInvalid')
  }
})

const loadStatus = async () => {
  try {
    status.value = await activationCodeAPI.verify()
  } catch (error) {
    console.error('Failed to load activation status:', error)
  }
}

const handleAdd = async () => {
  const code = activationCode.value.trim()
  if (!code) {
    appStore.showError(t('activation.pleaseEnterCode'))
    return
  }

  submitting.value = true
  errorMessage.value = ''
  addResult.value = null

  try {
    addResult.value = await activationCodeAPI.add(code)
    activationCode.value = ''
    appStore.showSuccess(t('activation.addSuccess'))
    // 余额已变化，刷新用户信息与激活码状态
    await authStore.refreshUser()
    await loadStatus()
  } catch (error: any) {
    errorMessage.value =
      error.response?.data?.message || error.message || t('activation.addFailed')
  } finally {
    submitting.value = false
  }
}

onMounted(() => {
  loadStatus()
})
</script>
