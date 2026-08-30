import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ActivationCodesView from '../ActivationCodesView.vue'

const { list, create, update, remove, getUserLoginIPs, resetUserLoginIPs, showError, showSuccess } =
  vi.hoisted(() => ({
    list: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    remove: vi.fn(),
    getUserLoginIPs: vi.fn(),
    resetUserLoginIPs: vi.fn(),
    showError: vi.fn(),
    showSuccess: vi.fn()
  }))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    activationCodes: {
      list,
      create,
      update,
      delete: remove,
      getUserLoginIPs,
      resetUserLoginIPs
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: vi.fn().mockResolvedValue(true) })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const AppLayoutStub = { template: '<div><slot /></div>' }
const TablePageLayoutStub = {
  template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
}
const DataTableStub = {
  props: ['data'],
  template: `
    <div>
      <div v-for="row in data" :key="row.id">
        <slot name="cell-code" :row="row" :value="row.code" />
        <slot name="cell-amount" :row="row" :value="row.amount" />
        <slot name="cell-status" :row="row" :value="row.status" />
        <slot name="cell-used_by" :row="row" :value="row.used_by" />
        <slot name="cell-actions" :row="row" />
      </div>
    </div>
  `
}
const BaseDialogStub = {
  props: ['show'],
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
}
const SelectStub = { props: ['modelValue', 'options'], template: '<select />' }
const IconStub = { props: ['name'], template: '<i />' }

const mountView = () =>
  mount(ActivationCodesView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        Pagination: true,
        Select: SelectStub,
        Icon: IconStub
      }
    }
  })

const unusedCode = {
  id: 1,
  code: 'FREECODE',
  amount: 50,
  status: 'unused',
  starts_at: null,
  expires_at: null,
  used_by: null,
  used_at: null,
  created_at: '2026-08-30T10:00:00Z',
  updated_at: '2026-08-30T10:00:00Z',
  notes: ''
}

describe('admin ActivationCodesView', () => {
  beforeEach(() => {
    list.mockReset()
    create.mockReset()
    update.mockReset()
    remove.mockReset()
    getUserLoginIPs.mockReset()
    resetUserLoginIPs.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    list.mockResolvedValue({ items: [unusedCode], total: 1, page: 1, page_size: 20, pages: 1 })
  })

  it('lists codes and renders their status', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(list).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('FREECODE')
    expect(wrapper.text()).toContain('admin.activation.statusUnused')
  })

  it('creates a batch with the period converted to unix seconds', async () => {
    create.mockResolvedValue([unusedCode, { ...unusedCode, id: 2, code: 'SECOND' }])

    const wrapper = mountView()
    await flushPromises()

    await wrapper.find('.btn-primary').trigger('click')
    await flushPromises()

    const form = wrapper.find('#create-activation-form')
    const inputs = form.findAll('input')
    // 顺序：code / amount / starts_at / expires_at / count
    await inputs[1].setValue('88')
    await inputs[2].setValue('2026-09-01T00:00')
    await inputs[3].setValue('2026-12-01T00:00')
    await inputs[4].setValue('2')
    await form.trigger('submit')
    await flushPromises()

    expect(create).toHaveBeenCalledTimes(1)
    const payload = create.mock.calls[0][0]
    expect(payload.amount).toBe(88)
    expect(payload.count).toBe(2)
    // 批量生成时不带指定码
    expect(payload.code).toBeUndefined()
    expect(payload.starts_at).toBe(Math.floor(new Date('2026-09-01T00:00').getTime() / 1000))
    expect(payload.expires_at).toBe(Math.floor(new Date('2026-12-01T00:00').getTime() / 1000))
    expect(showSuccess).toHaveBeenCalled()
    // 批量结果对话框展示生成的码
    const textarea = wrapper.find('textarea[readonly]')
    expect(textarea.exists()).toBe(true)
    expect((textarea.element as HTMLTextAreaElement).value).toContain('SECOND')
  })

  it('rejects an expiry that is not after the effective date', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.find('.btn-primary').trigger('click')
    await flushPromises()

    const form = wrapper.find('#create-activation-form')
    const inputs = form.findAll('input')
    await inputs[2].setValue('2026-12-01T00:00')
    await inputs[3].setValue('2026-09-01T00:00')
    await form.trigger('submit')
    await flushPromises()

    expect(create).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('admin.activation.invalidPeriod')
  })

  it('loads and resets login IPs for a bound code', async () => {
    list.mockResolvedValue({
      items: [{ ...unusedCode, status: 'used', used_by: 7, used_at: '2026-09-01T10:00:00Z' }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    getUserLoginIPs.mockResolvedValue({
      login_ips: [
        { ip: '1.2.3.4', created_at: '2026-09-01T10:00:00Z', last_seen_at: '2026-09-02T09:00:00Z' }
      ],
      login_ip_used: 1,
      login_ip_limit: 2
    })
    resetUserLoginIPs.mockResolvedValue({ removed: 1 })

    const wrapper = mountView()
    await flushPromises()

    await wrapper.find('button[title="admin.activation.loginIPs"]').trigger('click')
    await flushPromises()

    expect(getUserLoginIPs).toHaveBeenCalledWith(7)
    expect(wrapper.text()).toContain('1.2.3.4')

    const resetButton = wrapper
      .findAll('button')
      .find((button) => button.text() === 'admin.activation.resetLoginIPs')
    expect(resetButton).toBeDefined()
    await resetButton!.trigger('click')
    await flushPromises()

    expect(resetUserLoginIPs).toHaveBeenCalledWith(7)
    expect(getUserLoginIPs).toHaveBeenCalledTimes(2)
  })
})
