import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ActivationCodeView from '../ActivationCodeView.vue'

const { verify, add, refreshUser, showError, showSuccess } = vi.hoisted(() => ({
  verify: vi.fn(),
  add: vi.fn(),
  refreshUser: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api', () => ({
  activationCodeAPI: { verify, add }
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ refreshUser })
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const AppLayoutStub = { template: '<div><slot /></div>' }
const IconStub = { props: ['name'], template: '<i />' }

const mountView = () =>
  mount(ActivationCodeView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        Icon: IconStub
      }
    }
  })

const boundStatus = {
  bound: true,
  valid: true,
  reason: '',
  code: {
    id: 1,
    code: 'ABCD1234',
    amount: 100,
    status: 'used',
    starts_at: null,
    expires_at: '2026-12-01T00:00:00Z',
    used_by: 7,
    used_at: '2026-09-01T10:00:00Z',
    created_at: '2026-08-30T10:00:00Z',
    updated_at: '2026-09-01T10:00:00Z'
  },
  login_ips: [
    { ip: '1.2.3.4', created_at: '2026-09-01T10:00:00Z', last_seen_at: '2026-09-02T09:00:00Z' }
  ],
  login_ip_used: 1,
  login_ip_limit: 2
}

describe('user ActivationCodeView', () => {
  beforeEach(() => {
    verify.mockReset()
    add.mockReset()
    refreshUser.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
  })

  it('renders the bound activation code and its login IPs', async () => {
    verify.mockResolvedValue(boundStatus)

    const wrapper = mountView()
    await flushPromises()

    expect(verify).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('ABCD1234')
    expect(wrapper.text()).toContain('activation.statusValid')
    expect(wrapper.text()).toContain('1.2.3.4')
  })

  it('shows the invalid reason when the bound code expired', async () => {
    verify.mockResolvedValue({
      ...boundStatus,
      valid: false,
      reason: 'EXPIRED'
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('activation.statusExpired')
  })

  it('adds a code, then refreshes user and status', async () => {
    verify.mockResolvedValue({
      bound: false,
      valid: false,
      reason: 'NOT_BOUND',
      code: null,
      login_ips: [],
      login_ip_used: 0,
      login_ip_limit: 2
    })
    add.mockResolvedValue({
      code: boundStatus.code,
      amount: 100,
      new_balance: 109.05,
      replaced: 1,
      login_ip_reset: true
    })

    const wrapper = mountView()
    await flushPromises()

    await wrapper.find('#activation-code').setValue('  abcd1234  ')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(add).toHaveBeenCalledWith('abcd1234')
    expect(refreshUser).toHaveBeenCalledTimes(1)
    expect(verify).toHaveBeenCalledTimes(2)
    expect(showSuccess).toHaveBeenCalled()
    expect(wrapper.text()).toContain('109.05')
    expect(wrapper.text()).toContain('activation.previousReplaced')
  })

  it('surfaces the backend message when adding fails', async () => {
    verify.mockResolvedValue(boundStatus)
    add.mockRejectedValue({ response: { data: { message: 'activation code already used' } } })

    const wrapper = mountView()
    await flushPromises()

    await wrapper.find('#activation-code').setValue('TAKEN')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('activation code already used')
  })
})
