/**
 * Activation code API endpoints (user side)
 * 校验当前用户激活码有效性 + 添加激活码（绑定并充值）
 */

import { apiClient } from './client'
import type { ActivationStatus, AddActivationCodeResponse } from '@/types'

/**
 * 校验当前用户的激活码是否有效（无入参，仅凭 token）
 */
export async function verify(): Promise<ActivationStatus> {
  const { data } = await apiClient.get<ActivationStatus>('/activation-code/verify')
  return data
}

/**
 * 添加激活码：绑定到当前账户并把激活码配置的金额充入余额，
 * 之前绑定的激活码会被替换掉
 */
export async function add(code: string): Promise<AddActivationCodeResponse> {
  const { data } = await apiClient.post<AddActivationCodeResponse>('/activation-code', { code })
  return data
}

export const activationCodeAPI = {
  verify,
  add
}

export default activationCodeAPI
