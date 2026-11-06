import { useCallback, useEffect, useState } from 'react'
import { api, ApiError } from './api'

export type Direction = 'fiat_to_crypto' | 'crypto_to_fiat'
export type ProfileVersion = { version: number; legal_name: string; created_at: string }
export type ChannelBinding = {
  id: string; instance_id: string; provider_code: string; instance_code: string; environment: string
  kind: 'enrollment' | 'virtual_account'; label: string; masked_external_id: string; asset: string
  profile_version: number; created_at: string
}
export type TargetVersion = {
  id: string; target_id: string; version: number; kind: 'bank' | 'chain'; label: string
  masked_destination: string; asset: string; network: string; review_state: string; created_at: string
}
export type Resources = { fiat_asset: 'USD' | 'HKD'; profiles: ProfileVersion[]; bindings: ChannelBinding[]; targets: TargetVersion[] }
export type Quote = { rate: string; destination_amount: string; expires_at: string; evidence: string }
export type OrderSummary = {
  id: string; order_number: string; merchant_id: string; direction: Direction; version: number
  source_amount: string; source_asset: string; destination_asset: string; status: string
  hold_reason: string; next_action: string; created_at: string; operator_name: string
}
export type Order = OrderSummary & {
  current_version: number; profile_version: number; target_version_id?: string; channel_binding_id?: string
  pinned_profile: ProfileVersion; pinned_target?: TargetVersion
  pinned_binding?: ChannelBinding
  instruction: string; quote?: Quote & { version: number; expired: boolean }; version_created_at: string; recorded_by_name: string
  stages: { code: string; label: string; status: string; reason: string; created_at: string }[]
  steps: { id: string; action: string; label: string; status: string; instance_id?: string; binding_id?: string }[]
}

export const directions: Record<Direction, string> = {
  fiat_to_crypto: '法币转数币', crypto_to_fiat: '数币转法币',
}
export function displayTime(value: string) {
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}
export function errorMessage(error: unknown) {
  if (error instanceof ApiError) return error.message
  if (error instanceof TypeError) return '无法连接服务，请检查网络后重试。'
  return error instanceof Error ? error.message : '操作失败，请重试。'
}

export function useServerRead<T>(path: string | null, revision: number = 0) {
  const [refreshNo, setRefreshNo] = useState(0)
  const [state, setState] = useState<{ key: string; data?: T; error?: string } | null>(null)
  const key = JSON.stringify([path, refreshNo, revision])
  useEffect(() => {
    if (!path) return
    let active = true
    api<T>(path).then(data => {
      if (active) setState({ key, data })
    }).catch(error => {
      if (active) setState({ key, error: errorMessage(error) })
    })
    return () => { active = false }
  }, [key, path])
  const current = state?.key === key ? state : null
  const reload = useCallback(() => setRefreshNo(n => n + 1), [])
  return { data: current?.data, error: current?.error || '', loading: Boolean(path) && !current, reload }
}

export function targetCaption(target: Omit<TargetVersion, 'created_at'>) {
  const asset = [target.asset, target.network].filter(Boolean).join(' / ')
  return `${target.label} · ${asset} · ${target.masked_destination} · V${target.version}`
}
export function bindingCaption(binding: ChannelBinding) {
  return `${binding.label} · ${binding.provider_code} / ${binding.instance_code} · ${binding.masked_external_id}`
}
