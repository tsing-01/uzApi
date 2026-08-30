/**
 * Admin Activation Codes API endpoints
 * 激活码：管理员创建（生效日期 + 充值金额），用户绑定时充入余额
 */

import { apiClient } from '../client'
import type {
  ActivationCode,
  CreateActivationCodeRequest,
  UpdateActivationCodeRequest,
  UserLoginIPsResponse,
  BasePaginationResponse
} from '@/types'

export async function list(
  page: number = 1,
  pageSize: number = 20,
  filters?: {
    status?: string
    search?: string
    sort_by?: string
    sort_order?: 'asc' | 'desc'
  },
  options?: {
    signal?: AbortSignal
  }
): Promise<BasePaginationResponse<ActivationCode>> {
  const { data } = await apiClient.get<BasePaginationResponse<ActivationCode>>(
    '/admin/activation-codes',
    {
      params: { page, page_size: pageSize, ...filters },
      signal: options?.signal
    }
  )
  return data
}

export async function getById(id: number): Promise<ActivationCode> {
  const { data } = await apiClient.get<ActivationCode>(`/admin/activation-codes/${id}`)
  return data
}

/** 创建激活码，返回本次创建出的所有码（count > 1 时为批量结果） */
export async function create(request: CreateActivationCodeRequest): Promise<ActivationCode[]> {
  const { data } = await apiClient.post<ActivationCode[]>('/admin/activation-codes', request)
  return data
}

export async function update(
  id: number,
  request: UpdateActivationCodeRequest
): Promise<ActivationCode> {
  const { data } = await apiClient.put<ActivationCode>(`/admin/activation-codes/${id}`, request)
  return data
}

export async function deleteCode(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`/admin/activation-codes/${id}`)
  return data
}

/** 查看某个用户已占用的登录 IP */
export async function getUserLoginIPs(userId: number): Promise<UserLoginIPsResponse> {
  const { data } = await apiClient.get<UserLoginIPsResponse>(`/admin/users/${userId}/login-ips`)
  return data
}

/** 清空某个用户的登录 IP 记录（用户换网络被拦截时解封） */
export async function resetUserLoginIPs(userId: number): Promise<{ removed: number }> {
  const { data } = await apiClient.delete<{ removed: number }>(`/admin/users/${userId}/login-ips`)
  return data
}

const activationCodesAPI = {
  list,
  getById,
  create,
  update,
  delete: deleteCode,
  getUserLoginIPs,
  resetUserLoginIPs
}

export default activationCodesAPI
